package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"artex/llmrec"
	"artex/norma/harness"
	"artex/norma/llm"
)

func TestReasonHintCoversEveryTerminalReason(t *testing.T) {
	all := []harness.TerminalReason{
		harness.ReasonCompleted, harness.ReasonBlockingLimit, harness.ReasonImageError,
		harness.ReasonModelError, harness.ReasonAbortedStreaming, harness.ReasonAbortedTools,
		harness.ReasonPromptTooLong, harness.ReasonStopHookPrevented, harness.ReasonHookStopped,
		harness.ReasonMaxTurns, harness.ReasonTimeout,
	}
	for _, reason := range all {
		if strings.TrimSpace(reasonHint[reason]) == "" {
			t.Errorf("terminal reason %q has no explanation", reason)
		}
	}
}

func TestAbortCausePropagatesThroughRunContextChain(t *testing.T) {
	type key struct{}
	execCtx, cancelExec := context.WithCancelCause(context.Background())
	workCtx, cancelWork := context.WithCancelCause(execCtx)
	defer cancelWork(nil)
	valued := context.WithValue(workCtx, key{}, "task-1")
	runCtx, cancelRun := context.WithTimeoutCause(valued, time.Hour, AbortRunHardTimeout)
	defer cancelRun()

	cancelExec(AbortPausedByUser)
	code, _, text, ok := AbortReason(runCtx)
	if !ok || code != "paused_by_user" {
		t.Fatalf("code=%q ok=%v, want paused_by_user", code, ok)
	}
	if !strings.Contains(text, "frontier") {
		t.Fatalf("cause detail did not propagate: %q", text)
	}
}

func TestAbortReasonFallbacks(t *testing.T) {
	if _, _, _, ok := AbortReason(context.Background()); ok {
		t.Fatal("live context must not report an abort reason")
	}
	plain, cancel := context.WithCancel(context.Background())
	cancel()
	if code, _, _, ok := AbortReason(plain); !ok || code != "canceled_no_cause" {
		t.Fatalf("code=%q ok=%v, want canceled_no_cause", code, ok)
	}
	timed, cancelTimed := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelTimed()
	<-timed.Done()
	if code, _, _, _ := AbortReason(timed); code != "deadline_exceeded" {
		t.Fatalf("code=%q, want deadline_exceeded", code)
	}
}

func TestTerminalTextAbortedNamesCauseAndHangingTool(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortKilledByPlanner)
	trace := &runTrace{startedAt: time.Now().Add(-90 * time.Second)}
	trace.start("tu_1", "Bash", `{"command":"nmap -p- 10.0.0.1"}`)
	term := &harness.Terminal{
		Reason: harness.ReasonAbortedTools,
		Err:    context.Canceled,
		Turns:  7,
		Usage:  llm.Usage{InputTokens: 1200, OutputTokens: 340},
	}
	summary, detail := terminalText(ctx, term, trace)
	if !strings.Contains(summary, "planner") || strings.Contains(summary, "\n") {
		t.Fatalf("unexpected summary: %q", summary)
	}
	for _, want := range []string{"killed_by_planner", "aborted_tools", "7 turns", "Bash", "no result returned", "1200"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestTerminalTextDirectContextCancellationKeepsCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortChatStoppedByUser)
	summary, detail := terminalText(ctx, &harness.Terminal{Err: context.Canceled}, &runTrace{startedAt: time.Now()})
	if !strings.Contains(summary, "user stopped") || !strings.Contains(detail, "chat_stopped_by_user") {
		t.Fatalf("direct context cancellation lost cause: %q\n%s", summary, detail)
	}
	if !strings.Contains(detail, "context_canceled") {
		t.Fatalf("missing synthesized terminal label: %s", detail)
	}
}

func TestTerminalTextAbortedPreservesPartialOutput(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortPausedByUser)
	summary, detail := terminalText(ctx, &harness.Terminal{
		Reason: harness.ReasonAbortedStreaming,
		Text:   "a half-generated answer",
	}, &runTrace{startedAt: time.Now()})
	if !strings.Contains(summary, "user paused") {
		t.Fatalf("abort summary lost cause: %q", summary)
	}
	if !strings.Contains(detail, "a half-generated answer") {
		t.Fatalf("abort detail lost partial output: %s", detail)
	}
}

func TestTerminalTextCompletedToolNotBlamed(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortShutdown)
	trace := &runTrace{startedAt: time.Now()}
	trace.start("tu_1", "Read", `{"path":"/etc/hosts"}`)
	trace.done("tu_1")
	_, detail := terminalText(ctx, &harness.Terminal{Reason: harness.ReasonAbortedStreaming}, trace)
	if strings.Contains(detail, "no result returned") || !strings.Contains(detail, "returned normally") {
		t.Fatalf("completed tool was blamed:\n%s", detail)
	}
}

func TestTerminalTextNonAbortReasons(t *testing.T) {
	trace := &runTrace{startedAt: time.Now()}
	summary, _ := terminalText(context.Background(), &harness.Terminal{Reason: harness.ReasonMaxTurns}, trace)
	if !strings.Contains(summary, "run budget limit") || strings.Contains(summary, "interrupt") {
		t.Fatalf("unexpected max_turns summary: %q", summary)
	}
	summary, detail := terminalText(context.Background(), &harness.Terminal{
		Reason: harness.ReasonModelError, Err: errors.New("429 rate limited"),
	}, trace)
	if !strings.Contains(summary, "model_error") || !strings.Contains(detail, "429 rate limited") {
		t.Fatalf("model_error detail incomplete: %q\n%s", summary, detail)
	}
}

func TestAbortCausesAreWellFormed(t *testing.T) {
	all := []*AbortCause{
		AbortPausedByUser, AbortPausedByOrchestrator, AbortTaskDeleted, AbortPausedOnReload,
		AbortGoalMet, AbortSettleDrainTimeout, AbortKilledByPlanner, AbortWorkPausedByUser,
		AbortWorkCancelledByUser, AbortWorkFinished, AbortPausedRaceGuard,
		AbortChatStoppedByUser, AbortChatPausedWithTask, AbortChatTurnFinished,
		AbortShutdown, AbortRunHardTimeout,
	}
	seen := map[string]bool{}
	for _, abort := range all {
		switch {
		case abort.Code == "" || seen[abort.Code]:
			t.Errorf("missing or duplicate code: %q", abort.Code)
		case abort.Short == "" || utf8.RuneCountInString(abort.Short) > 40:
			t.Errorf("%s has invalid short text: %q", abort.Code, abort.Short)
		case utf8.RuneCountInString(abort.Text) <= utf8.RuneCountInString(abort.Short):
			t.Errorf("%s detail must be longer than short text", abort.Code)
		}
		seen[abort.Code] = true
	}
}

func TestFirstLineCapsByRuneNotByte(t *testing.T) {
	if got := firstLine(strings.Repeat("恢复", 10), 5); got != "恢复恢复恢…" {
		t.Fatalf("firstLine split a Unicode character: %q", got)
	}
	if got := firstLine("头\n尾", 100); got != "头" {
		t.Fatalf("firstLine did not stop at newline: %q", got)
	}
}

// stopProvider answers every call with a fixed stop reason and output tokens.
type stopProvider struct {
	stop string
	out  int
}

func (p stopProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p stopProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{Role: llm.RoleAssistant}, p.stop, llm.Usage{InputTokens: 18866, OutputTokens: p.out}, nil
}

// ctxWithStop returns a context whose stop trace saw one call ending with stop.
func ctxWithStop(t *testing.T, stop string, out int) context.Context {
	t.Helper()
	ctx, _ := llmrec.WithStopTrace(context.Background())
	rec := llmrec.Wrap(stopProvider{stop: stop, out: out}, nil, "claude-opus-5-5", "test", "", "", func() bool { return false })
	if _, _, _, err := rec.Complete(ctx, llm.CompletionRequest{}); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestTerminalTextDistinguishesRefusalFromEmptyReply(t *testing.T) {
	cases := []struct {
		name, stop, text string
		wantKind         stopKind
		wantSum          []string
		wantDetail       []string
		notSum           []string
	}{
		{name: "anthropic refusal", stop: "refusal", wantKind: stopRefusal,
			wantSum:    []string{"Model refused", "`refusal`", "not an API error"},
			wantDetail: []string{"`refused`", "Provider stop reason", "the harness reported `completed`"},
			notSum:     []string{"Empty reply", "No text summary"}},
		{name: "openai content filter", stop: "content_filter", wantKind: stopRefusal,
			wantSum: []string{"Model refused", "`content_filter`"}},
		{name: "refusal with partial text", stop: "refusal", text: "I can start by", wantKind: stopRefusal,
			wantSum:    []string{"Model refused"},
			wantDetail: []string{"Partial output before the refusal", "I can start by"}},
		{name: "normal end with nothing", stop: "end_turn", wantKind: stopEmpty,
			wantSum:    []string{"Empty reply", "`end_turn`", "0 output tokens"},
			wantDetail: []string{"`empty_reply`"},
			notSum:     []string{"refused"}},
		{name: "openai stop with nothing", stop: "stop", wantKind: stopEmpty,
			wantSum: []string{"Empty reply"}},
		{name: "max tokens before output", stop: "max_tokens", wantKind: stopUnexpected,
			wantSum:    []string{"Unexpected stop", "`max_tokens`"},
			wantDetail: []string{"`unexpected_stop`"},
			notSum:     []string{"refused"}},
		{name: "no stop reason reported", stop: "", wantKind: stopUnexpected,
			wantSum: []string{"Unexpected stop", "none reported"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxWithStop(t, tc.stop, 0)
			term := &harness.Terminal{Reason: harness.ReasonCompleted, Text: tc.text, Turns: 1}
			ps, ok := lastProviderStop(ctx, term)
			if !ok || ps.kind != tc.wantKind {
				t.Fatalf("lastProviderStop = %+v, %v; want kind %v", ps, ok, tc.wantKind)
			}
			sum, detail := terminalText(ctx, term, &runTrace{startedAt: time.Now()})
			for _, w := range tc.wantSum {
				if !strings.Contains(sum, w) {
					t.Errorf("summary %q missing %q", sum, w)
				}
			}
			for _, w := range tc.notSum {
				if strings.Contains(sum, w) {
					t.Errorf("summary %q must not contain %q", sum, w)
				}
			}
			for _, w := range tc.wantDetail {
				if !strings.Contains(detail, w) {
					t.Errorf("detail missing %q:\n%s", w, detail)
				}
			}
		})
	}
}

func TestLastProviderStopLeavesNormalRunsAlone(t *testing.T) {
	// A normal answer is not flagged.
	ctx := ctxWithStop(t, "end_turn", 120)
	if _, ok := lastProviderStop(ctx, &harness.Terminal{Reason: harness.ReasonCompleted, Text: "done"}); ok {
		t.Fatal("a completed run with text must not be flagged")
	}
	// Other terminal reasons keep their own explanation even after a refusal stop.
	ctx = ctxWithStop(t, "refusal", 0)
	if _, ok := lastProviderStop(ctx, &harness.Terminal{Reason: harness.ReasonModelError}); ok {
		t.Fatal("only completed runs are reclassified")
	}
	// Without a recorder-observed call the old wording stays.
	sum, _ := terminalText(context.Background(), &harness.Terminal{Reason: harness.ReasonCompleted}, &runTrace{})
	if !strings.Contains(sum, "No text summary, terminal state completed") {
		t.Fatalf("fallback summary changed: %q", sum)
	}
}
