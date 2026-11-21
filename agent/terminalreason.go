package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"artex/llmrec"
	"artex/norma/harness"
)

// runTrace retains the latest tool call so an interrupted run can identify the
// operation that was still in flight.
type runTrace struct {
	startedAt time.Time
	id        string
	name      string
	input     string
	at        time.Time
	pending   bool
}

func (t *runTrace) start(id, name, input string) {
	t.id, t.name, t.input, t.at, t.pending = id, name, input, time.Now(), true
}

func (t *runTrace) done(id string) {
	if id == t.id {
		t.pending = false
	}
}

var reasonHint = map[harness.TerminalReason]string{
	harness.ReasonCompleted:         "The model ended this turn normally but left no text summary; facts and assets are as recorded by this turn's tool calls",
	harness.ReasonMaxTurns:          "Hit the step limit (MaxTurns): the SDK ran wrap-up and wrote back facts and assets; the intent is marked exhausted so the planner can change direction and continue, rather than treating it as a failure",
	harness.ReasonTimeout:           "Hit the single-run wall-clock budget (MaxDuration): when time is up the running tool is interrupted and wrap-up starts in place, writing back the facts and assets identified; the intent is marked exhausted",
	harness.ReasonModelError:        "The model or API call failed (network, auth, rate limit, provider 5xx, etc.); after retries ran out the intent was marked blocked — a transport-layer failure means this intent was barely explored; check its execution process (get_worker_trace) before deciding whether to re-dispatch or change approach",
	harness.ReasonBlockingLimit:     "The context length hit the hard limit and the request was blocked before being sent; narrow the intent granularity or compress tool returns",
	harness.ReasonPromptTooLong:     "The prompt is too long and context-compaction retries are exhausted; cannot continue",
	harness.ReasonImageError:        "The current model does not support this turn's multimodal content; switch to a vision-capable model or avoid tools returning images",
	harness.ReasonStopHookPrevented: "A Stop hook prevented this turn from ending and it could not continue afterwards; check whether the task's Guard rules are too strict",
	harness.ReasonHookStopped:       "A tool or hook stopped execution on purpose, e.g. an out-of-scope target or a forbidden command; check the block explanation in the last tool_result",
	harness.ReasonAbortedStreaming:  "The run was cancelled while the model output was streaming",
	harness.ReasonAbortedTools:      "The run was cancelled during tool execution",
}

// terminalText renders a terminal event with no final text into a compact summary
// and a Markdown detail block.
func terminalText(ctx context.Context, term *harness.Terminal, tr *runTrace) (string, string) {
	reason := term.Reason
	aborted := reason == harness.ReasonAbortedStreaming || reason == harness.ReasonAbortedTools
	// Prompt may return ctx.Err directly without a terminal event. Preserve the
	// cancellation cause instead of falling back to an empty/unknown terminal reason.
	if reason == "" && ctx.Err() != nil {
		aborted = true
	}

	var sum string
	if aborted {
		_, short, _, ok := AbortReason(ctx)
		if !ok {
			short = "could not get the cancellation cause"
		}
		stage := "during execution"
		switch reason {
		case harness.ReasonAbortedStreaming:
			stage = "during model output"
		case harness.ReasonAbortedTools:
			stage = "during tool execution"
		}
		sum = "(Run interrupted: " + short + "; stopped " + stage + progressSuffix(term, tr) + ", not finished)"
	} else if reason == harness.ReasonMaxTurns || reason == harness.ReasonTimeout {
		sum = "(Hit the run budget limit (" + string(reason) + "), wrapped up and wrote back facts" + progressSuffix(term, tr) + "; no text summary this time)"
	} else if ps, ok := lastProviderStop(ctx, term); ok {
		sum = ps.summary()
	} else {
		hint := terminalReasonHint(reason)
		sum = "(No text summary, terminal state " + terminalReasonLabel(reason) + ": " + firstLine(hint, 80) + ")"
	}

	var b strings.Builder
	b.WriteString(sum)
	b.WriteString("\n\n")
	displayReason := terminalReasonLabel(reason)
	if ps, ok := lastProviderStop(ctx, term); ok && !aborted {
		fmt.Fprintf(&b, "- **Terminal state**: `%s` - %s\n", ps.kind.label(), ps.kind.hint())
		fmt.Fprintf(&b, "- **Provider stop reason**: %s (the harness reported `%s`)\n", ps.rawLabel(), displayReason)
		if ps.kind == stopRefusal && strings.TrimSpace(term.Text) != "" {
			b.WriteString("- **Partial output before the refusal**:\n\n")
			b.WriteString(term.Text)
			b.WriteString("\n\n")
		}
	} else {
		fmt.Fprintf(&b, "- **Terminal state**: `%s` - %s\n", displayReason, terminalReasonHint(reason))
		if !aborted {
			if raw, _, seen := llmrec.StopTraceFrom(ctx).Last(); seen {
				fmt.Fprintf(&b, "- **Provider stop reason**: %s\n", providerStop{raw: raw}.rawLabel())
			}
		}
	}
	if aborted {
		code, _, why, ok := AbortReason(ctx)
		if ok {
			fmt.Fprintf(&b, "- **Interruption cause** (`%s`): %s\n", code, why)
		} else {
			b.WriteString("- **Interruption cause**: unavailable; the canceller may not have attached a named cause via context.WithCancelCause\n")
		}
	}
	if term.Err != nil {
		fmt.Fprintf(&b, "- **Underlying error**: `%v`\n", term.Err)
	}
	if aborted && strings.TrimSpace(term.Text) != "" {
		b.WriteString("- **Partial output generated before cancellation**:\n\n")
		b.WriteString(term.Text)
		b.WriteString("\n\n")
	}
	if term.Turns > 0 {
		fmt.Fprintf(&b, "- **Executed**: %d model turns\n", term.Turns)
	}
	if !tr.startedAt.IsZero() {
		fmt.Fprintf(&b, "- **Run duration**: %s\n", roundDur(time.Since(tr.startedAt)))
	}
	if u := term.Usage; u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens > 0 {
		fmt.Fprintf(&b, "- **Cumulative tokens**: input %d / output %d / cache read %d / cache write %d\n",
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
	if tr.name == "" {
		b.WriteString("- **Tool calls**: this run ended before issuing any tool call\n")
	} else if tr.pending {
		fmt.Fprintf(&b, "- **Tool running at interruption**: `%s` (ran for %s, **no result returned**)\n\n  ```json\n  %s\n  ```\n",
			tr.name, roundDur(time.Since(tr.at)), firstLine(tr.input, 300))
	} else {
		fmt.Fprintf(&b, "- **Last tool before interruption**: `%s` (returned normally)\n", tr.name)
	}
	return sum, b.String()
}

func terminalReasonLabel(reason harness.TerminalReason) string {
	if reason == "" {
		return "context_canceled"
	}
	return string(reason)
}

func terminalReasonHint(reason harness.TerminalReason) string {
	if hint := reasonHint[reason]; hint != "" {
		return hint
	}
	if reason == "" {
		return "the run's context was cancelled, but no Terminal event was produced underneath"
	}
	return "unknown terminal state; the harness may have added a TerminalReason, please add it to reasonHint"
}

func progressSuffix(term *harness.Terminal, tr *runTrace) string {
	var parts []string
	if term.Turns > 0 {
		parts = append(parts, fmt.Sprintf("%d turns", term.Turns))
	}
	if !tr.startedAt.IsZero() {
		parts = append(parts, roundDur(time.Since(tr.startedAt)))
	}
	if len(parts) == 0 {
		return ""
	}
	return ", ran " + strings.Join(parts, " / ")
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	case d < time.Hour:
		return d.Round(time.Second).String()
	default:
		return d.Round(time.Minute).String()
	}
}

// stopKind classifies the provider's raw stop reason for a run the harness
// reported as ReasonCompleted. The harness folds every non-tool stop into that
// one reason, so a safety refusal and a genuinely empty answer both arrive as
// "completed"; the raw value (seen by llmrec.Recorder) is what tells them apart.
type stopKind int

const (
	// stopRefusal: the provider's safety system ended the reply (Anthropic
	// "refusal", OpenAI "content_filter", Gemini-style "safety"). Not an API
	// error — the call succeeded, the model declined.
	stopRefusal stopKind = iota + 1
	// stopEmpty: the model ended its turn normally but wrote no text and called
	// no tool.
	stopEmpty
	// stopUnexpected: any other stop reason with no output (max_tokens before any
	// text, a missing stop reason, a value this code does not know).
	stopUnexpected
)

func classifyStop(raw string) stopKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "refusal", "content_filter", "safety", "prohibited_content":
		return stopRefusal
	case "end_turn", "stop", "stop_sequence", "completed":
		return stopEmpty
	default:
		return stopUnexpected
	}
}

func (k stopKind) label() string {
	switch k {
	case stopRefusal:
		return "refused"
	case stopEmpty:
		return "empty_reply"
	default:
		return "unexpected_stop"
	}
}

func (k stopKind) hint() string {
	switch k {
	case stopRefusal:
		return "The provider's safety system stopped the model's reply. The API call succeeded, so this is not a network, auth or provider error. Retrying the same prompt usually gets the same result; reword the intent or give it more authorization context"
	case stopEmpty:
		return "The model ended its turn normally but produced no text and no tool call. The API call succeeded; this usually points at the request (model ID, gateway, unsupported parameters) rather than the task"
	default:
		return "The provider ended the reply with a stop reason that is neither a normal finish nor a refusal, before any output; check this call in llm_records"
	}
}

// providerStop is how the provider ended the call that finished a run.
type providerStop struct {
	kind         stopKind
	raw          string
	outputTokens int
}

// lastProviderStop reports how the provider ended a ReasonCompleted run, for the
// two cases where the plain "completed" label misleads: a refusal (with or
// without partial text) and a reply that produced no text at all. ok is false for
// every other run, and when no llmrec.Recorder saw the call.
func lastProviderStop(ctx context.Context, term *harness.Terminal) (providerStop, bool) {
	if term == nil || term.Reason != harness.ReasonCompleted {
		return providerStop{}, false
	}
	raw, out, seen := llmrec.StopTraceFrom(ctx).Last()
	if !seen {
		return providerStop{}, false
	}
	ps := providerStop{kind: classifyStop(raw), raw: raw, outputTokens: out}
	if ps.kind != stopRefusal && strings.TrimSpace(term.Text) != "" {
		return providerStop{}, false // a normal answer: nothing to explain
	}
	return ps, true
}

func (p providerStop) rawLabel() string {
	if strings.TrimSpace(p.raw) == "" {
		return "none reported"
	}
	return "`" + p.raw + "`"
}

func (p providerStop) summary() string {
	tokens := fmt.Sprintf("%d output tokens", p.outputTokens)
	switch p.kind {
	case stopRefusal:
		return "(Model refused: the provider's safety system stopped the reply (stop reason " + p.rawLabel() + ", " + tokens + "); this is a refusal, not an API error)"
	case stopEmpty:
		return "(Empty reply: the model ended normally (stop reason " + p.rawLabel() + ") but wrote no text and called no tool, " + tokens + ")"
	default:
		return "(Unexpected stop: the provider ended the reply with stop reason " + p.rawLabel() + " before any output, " + tokens + ")"
	}
}
