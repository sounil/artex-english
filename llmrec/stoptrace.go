package llmrec

import (
	"context"
	"sync"
)

type stopTraceContextKey struct{}

// StopTrace remembers how the most recent LLM call of one agent run ended: the
// provider's raw stop reason and that call's output tokens.
//
// The harness reduces every non-tool stop to ReasonCompleted, so without this a
// safety refusal (Anthropic stop_reason "refusal", OpenAI finish_reason
// "content_filter") looks exactly like a model that finished normally and wrote
// nothing. The Recorder sits in front of every provider and sees the raw value;
// it writes it here so the agent's terminal summary can tell the cases apart.
//
// The Recorder writes from norma's stream goroutine while the agent reads after
// the run ends, so state is mutex-guarded.
type StopTrace struct {
	mu           sync.Mutex
	seen         bool
	stopReason   string
	outputTokens int
}

// WithStopTrace returns a context carrying a fresh StopTrace, plus the trace.
func WithStopTrace(ctx context.Context) (context.Context, *StopTrace) {
	t := &StopTrace{}
	return context.WithValue(ctx, stopTraceContextKey{}, t), t
}

// StopTraceFrom returns the StopTrace attached to ctx, or nil when none is.
func StopTraceFrom(ctx context.Context) *StopTrace {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(stopTraceContextKey{}).(*StopTrace)
	return t
}

// observe records the outcome of one LLM call; later calls overwrite earlier
// ones so Last always describes the call that ended the run. Nil-safe.
func (t *StopTrace) observe(stopReason string, outputTokens int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.seen, t.stopReason, t.outputTokens = true, stopReason, outputTokens
	t.mu.Unlock()
}

// Last reports the most recent call's raw stop reason and output tokens. ok is
// false when no call went through a Recorder (e.g. an unwrapped provider).
func (t *StopTrace) Last() (stopReason string, outputTokens int, ok bool) {
	if t == nil {
		return "", 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopReason, t.outputTokens, t.seen
}
