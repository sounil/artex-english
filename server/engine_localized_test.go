package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests that keep the "user-facing" messages among engine.go's task control
// and intent intervention errors in English. The English check reuses
// assertEnglishError (no Hangul, Han, or kana).
//
// Call-graph verdict (see the engine.go constant block comment):
//   - ControlWork's four errors (no running work, control already in progress, wait for
//     settle cancelled, wait for settle timed out) → applyIntentControl
//     (task_control.go) → controlIntent (server.go) → writeErr 409. User-facing only;
//     they never reach an agent tool (actool) path.
//   - runDetachedIntent's two errors (Worker not ready, resume CAS conflict) →
//     sendWorkerMessage (intent_intervention.go) → writeErr. User-facing only.
//   - Conversely, SteerWork/KillWork (steer_work/kill_work tool actool.Errorf) and
//     transitionIntentState (internal state transition logs) are agent input/logs and
//     are not covered by this test.

// TestControlWorkNoRunningWorkErrorLocalized drives ControlWork directly, without DB or
// engine, to confirm it really returns an English error when no work is running. The
// run==nil branch only looks at the e.work map, so NewEngine(nil) reaches it end to
// end. Because it wraps the errWorkControlConflict sentinel with %w, it also checks
// that the errors.Is relationship is preserved. If anyone reverts this literal to
// Korean or Chinese, it fails.
func TestControlWorkNoRunningWorkErrorLocalized(t *testing.T) {
	e := NewEngine(nil)
	err := e.ControlWork(context.Background(), 42, "pause")
	if err == nil {
		t.Fatal("ControlWork returned nil although no work is running")
	}
	if !errors.Is(err, errWorkControlConflict) {
		t.Fatalf("errors.Is(err, errWorkControlConflict) = false, err=%v", err)
	}
	assertEnglishError(t, "control_work_no_running", err.Error())
}

// TestEngineUserFacingErrorsLocalized pins the remaining user-facing format strings
// that sit behind engine/DB gates or goroutine timing and are hard to drive end to end
// (same approach as the gated paths of finding_retests and finding_traffic). Format
// strings are filled with representative args before the check.
func TestEngineUserFacingErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"work_control_busy", fmt.Errorf(errWorkControlBusyFmt, errWorkControlConflict, int64(42), "pause").Error()},
		{"work_control_wait", fmt.Errorf(errWorkControlWaitFmt, int64(42), "pause", context.DeadlineExceeded).Error()},
		{"detached_worker_not_ready", errDetachedWorkerNotReady},
		{"detached_state_conflict", fmt.Errorf(errIntentCtrlStateConflictFmt, db.ErrIntentStateConflict).Error()},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
