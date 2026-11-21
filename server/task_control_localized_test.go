package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests that keep the error responses of task_control.go's task/intent
// control API in English. The English check reuses assertEnglishError (no Hangul, Han,
// or kana) and response-body extraction reuses decodeErrorField from the
// task_categories tests (same package server).

// TestTaskControlErrorConstantsLocalized asserts that all 10 control response
// constants are English. The intent control path (applyIntentControl) goes through
// t.Store.GetNode (DB) on its first line, so it cannot be driven end to end on a
// DB-less host; those 6 are asserted on the constants themselves (following the
// intent_intervention and goals_api precedent). The task control and batch paths are
// checked by the two tests below through real function execution and HTTP bodies.
func TestTaskControlErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"task_deleting":          errTaskCtrlDeleting,
		"task_terminal_pause":    errTaskCtrlTerminalPause,
		"task_already_paused":    errTaskCtrlAlreadyPaused,
		"batch_size":             errTaskCtrlBatchSizeFmt,
		"intent_inherited":       errIntentCtrlInheritedReadonly,
		"intent_only_running":    errIntentCtrlOnlyRunningPause,
		"intent_only_paused":     errIntentCtrlOnlyPausedResume,
		"intent_state_conflict":  errIntentCtrlStateConflictFmt,
		"intent_only_deletable":  errIntentCtrlOnlyDeletable,
		"intent_reason_required": errIntentCtrlReasonRequired,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestTaskControlApplyResponsesLocalized really runs the pause control paths that
// finish without touching DB/engine state through applyTaskControl, confirming the
// constants actually reach the returned error. All three branches go s.m.Task (map
// lookup) → delete barrier → t.lifecycleSnapshot() (reads the in-memory Status/Paused
// fields directly), so one task in the tasks map plus an empty Engine runs them
// without Store or DB. The success path (actually pausing) goes through
// ApplyTaskPause (DB) and is not driven here — every error branch returns before it.
func TestTaskControlApplyResponsesLocalized(t *testing.T) {
	// Task in a terminal state: the isTerminalStatus(done) branch.
	terminalServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "done"}
		return &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}, tk
	}
	// Running task that is already paused: the lifecycle.Paused branch.
	pausedServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "running", Paused: true}
		return &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}, tk
	}
	// Task with the delete barrier raised: the IsDeleting branch.
	deletingServer := func() (*Server, *Task) {
		tk := &Task{ID: "1", Status: "running"}
		s := &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}
		s.engine.deleting.Store("1", true)
		return s, tk
	}

	cases := []struct {
		name  string
		setup func() (*Server, *Task)
		want  string
	}{
		{"terminal", terminalServer, errTaskCtrlTerminalPause},
		{"already-paused", pausedServer, errTaskCtrlAlreadyPaused},
		{"deleting", deletingServer, errTaskCtrlDeleting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, tk := c.setup()
			_, err := s.applyTaskControl(tk, "pause")
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if err.Error() != c.want {
				t.Fatalf("returned error = %q, want %q", err.Error(), c.want)
			}
			assertEnglishError(t, c.name, err.Error())
		})
	}
}

// TestControlTasksBatchResponsesLocalized checks the batch control endpoint's
// response body over real HTTP. (1) Empty task_ids produce a 400 + size error via
// writeErr before s.m is accessed, so a bare &Server{} reaches it. (2) Batch-pausing
// one terminal task returns 200 with the task control error carried verbatim in
// items[].error, confirming applyTaskControl errors propagate into the batch JSON.
func TestControlTasksBatchResponsesLocalized(t *testing.T) {
	t.Run("size-error", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/control",
			strings.NewReader(`{"task_ids":[],"action":"pause"}`))
		rec := httptest.NewRecorder()
		s.controlTasksBatch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		got := decodeErrorField(t, rec.Body.Bytes())
		want := fmt.Sprintf(errTaskCtrlBatchSizeFmt, maxBatchControlIDs)
		if got != want {
			t.Fatalf("response message = %q, want %q", got, want)
		}
		if !strings.Contains(got, "100") {
			t.Fatalf("size cap 100 is not reflected in the message: %q", got)
		}
		assertEnglishError(t, "size-error", got)
	})

	t.Run("item-error-surfaces", func(t *testing.T) {
		tk := &Task{ID: "1", Status: "done"}
		s := &Server{m: &Manager{tasks: map[string]*Task{"1": tk}}, engine: &Engine{}}
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/control",
			strings.NewReader(`{"task_ids":["1"],"action":"pause"}`))
		rec := httptest.NewRecorder()
		s.controlTasksBatch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var out struct {
			Items []struct {
				ID    string `json:"id"`
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
		}
		if len(out.Items) != 1 {
			t.Fatalf("items count = %d, want 1", len(out.Items))
		}
		if out.Items[0].OK || out.Items[0].Error != errTaskCtrlTerminalPause {
			t.Fatalf("items[0] = %+v, want error %q", out.Items[0], errTaskCtrlTerminalPause)
		}
		assertEnglishError(t, "item-error", out.Items[0].Error)
	})
}

// TestIntentStateConflictWrapsSentinel confirms the intent state-conflict message is
// English while still wrapping db.ErrIntentStateConflict verbatim with %w, so errors.Is
// identification keeps working. The enum value paused must be preserved verbatim.
func TestIntentStateConflictWrapsSentinel(t *testing.T) {
	err := fmt.Errorf(errIntentCtrlStateConflictFmt, db.ErrIntentStateConflict)
	if !errors.Is(err, db.ErrIntentStateConflict) {
		t.Fatalf("errors.Is(.., ErrIntentStateConflict) = false, the %%w wrapping is broken: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Fatalf("enum paused was not preserved in the message: %q", err.Error())
	}
	assertEnglishError(t, "state-conflict", err.Error())
}
