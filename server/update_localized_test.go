package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F3b (update.go): regression tests that keep the self-update endpoint's user-facing
// messages in English. The frontend (update-card in system/settings) renders
// progress.message, reason, and the writeErr body verbatim, so if these messages
// revert to Korean or Chinese, the update screen shows untranslated toasts and
// progress messages again. The assertEnglishError and decodeErrorField helpers are
// reused from the earlier F3b test files (same package server).

func TestUpdateMessageConstantsLocalized(t *testing.T) {
	assertEnglishError(t, "updateMsgPreparing", updateMsgPreparing)
	assertEnglishError(t, "updateMsgFailed", updateMsgFailed)
	assertEnglishError(t, "updateMsgStaged", updateMsgStaged)
	assertEnglishError(t, "updateErrInProgress", updateErrInProgress)
	assertEnglishError(t, "updateErrRollbackInProgress", updateErrRollbackInProgress)

	// Format-string constants are checked after filling the placeholders (%q/%s substituted, no CJK).
	notRelease := fmt.Sprintf(updateErrNotReleaseFmt, "v0.0.0-dev")
	assertEnglishError(t, "updateErrNotReleaseFmt", notRelease)
	if !strings.Contains(notRelease, "v0.0.0-dev") {
		t.Fatalf("updateErrNotReleaseFmt: version placeholder was not substituted: %q", notRelease)
	}
	latest := fmt.Sprintf(updateErrAlreadyLatestFmt, "v1.2.3")
	assertEnglishError(t, "updateErrAlreadyLatestFmt", latest)
	if !strings.Contains(latest, "v1.2.3") {
		t.Fatalf("updateErrAlreadyLatestFmt: version placeholder was not substituted: %q", latest)
	}
}

// Verifies through the real code path that the progress messages begin/finish emit
// over SSE are set to the English constants. Uses a local instance to avoid
// polluting the global updHub (no DB or network needed).
func TestUpdateProgressMessagesLocalized(t *testing.T) {
	h := &updateHub{subs: map[chan updateProgress]struct{}{}}

	if !h.begin("v1.2.3") {
		t.Fatal("begin must return true on the first call")
	}
	cur, running := h.snapshot()
	if !running {
		t.Fatal("must be running after begin")
	}
	if cur.Message != updateMsgPreparing {
		t.Fatalf("preparing message mismatch: %q", cur.Message)
	}
	if h.begin("v1.2.4") {
		t.Fatal("begin must return false while in progress")
	}

	h.finish(errors.New("download failed"))
	cur, running = h.snapshot()
	if running {
		t.Fatal("running must be cleared after finish")
	}
	if cur.Message != updateMsgFailed {
		t.Fatalf("failure message mismatch: %q", cur.Message)
	}

	h.finish(nil)
	cur, _ = h.snapshot()
	if cur.Message != updateMsgStaged {
		t.Fatalf("staged message mismatch: %q", cur.Message)
	}
}

// Verifies over real HTTP, without a DB, that updateRollback's "update in progress, cannot roll back" 409 response is English.
func TestUpdateRollbackInProgressLocalized(t *testing.T) {
	updHub.mu.Lock()
	prev := updHub.running
	updHub.running = true
	updHub.mu.Unlock()
	defer func() {
		updHub.mu.Lock()
		updHub.running = prev
		updHub.mu.Unlock()
	}()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/update/rollback", nil)
	rec := httptest.NewRecorder()
	s.updateRollback(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want status code 409, got %d (body %s)", rec.Code, rec.Body.Bytes())
	}
	msg := decodeErrorField(t, rec.Body.Bytes())
	assertEnglishError(t, "updateRollback in-progress 409", msg)
	if msg != updateErrRollbackInProgress {
		t.Fatalf("rollback rejection message mismatch: %q", msg)
	}
}
