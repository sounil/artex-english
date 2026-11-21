package server

import "testing"

// Regression test that keeps goals.go's task-resume (admitPausedTask) precondition
// error messages in English. The English check reuses assertEnglishError (no Hangul,
// Han, or kana). The admitTaskWhen(requirePaused=true) path that returns these two
// messages goes through the engine gates s.m.Task(), s.engine.IsDeleting() and
// beginTaskOperation() after taking s.concMu, so it cannot run end to end on a host
// without DB/engine; the constants themselves are pinned instead (same approach as the
// gated paths of finding_retests and finding_traffic). If anyone reverts these
// literals to Korean or Chinese, this test fails.
//
// Call-graph verdict (see the goals.go constant block comment): both messages are
// user-facing only, exposed solely through the single (server.go → writeErr 409) and
// batch (task_control.go → items[].error) task control responses. The orchestrator
// path (orchestration.go) is fixed to action="pause" and never reaches resume
// validation, so they are not agent input.
func TestGoalResumeErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"resume_terminal", errGoalResumeTerminal},
		{"resume_not_paused", errGoalResumeNotPaused},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
