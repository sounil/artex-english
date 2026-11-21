package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep the error responses of intent_intervention.go's Worker
// intervention API in English. The English check reuses assertEnglishError (no Hangul,
// Han, or kana).

// TestIntentInterventionErrorConstantsLocalized asserts that all 11 response constants
// are English. The lifecycle and intent-state paths need s.engine/Store setup and
// cannot run end to end on a DB-less host, so those messages are asserted on the
// constants themselves (following the conversations.go precedent). The 4 input
// validation paths are checked down to the response body by the HTTP test below.
func TestIntentInterventionErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"request_too_large", errIntentRequestTooLarge},
		{"message_empty", errIntentMessageEmpty},
		{"message_too_long", errIntentMessageTooLong},
		{"bad_request_id", errIntentBadRequestID},
		{"task_deleting", errIntentTaskDeleting},
		{"task_paused", errIntentTaskPaused},
		{"task_queued", errIntentTaskQueued},
		{"task_terminal", errIntentTaskTerminal},
		{"task_settling", errIntentTaskSettling},
		{"inherited_readonly", errIntentInheritedReadonly},
		{"not_paused", errIntentNotPaused},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// TestSendWorkerMessageInputValidationLocalized checks the 4 input-validation paths all
// the way to the real HTTP response body. These 4 paths of sendWorkerMessage look only
// at s.m.Task (map lookup) and the request body without touching s.engine/Store/DB, so
// a single task in the tasks map runs them end to end without a DB. It also confirms
// the constants actually reach the response.
func TestSendWorkerMessageInputValidationLocalized(t *testing.T) {
	s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}}

	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/1/message", strings.NewReader(body))
		req.SetPathValue("id", "t1")
		req.SetPathValue("iid", "1")
		rec := httptest.NewRecorder()
		s.sendWorkerMessage(rec, req)
		return rec
	}
	errBody := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		var resp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
		}
		return resp.Error
	}

	cases := []struct {
		name string
		body string
		code int
		want string
	}{
		// Valid JSON body over the 64KB limit. MaxBytesReader reports the overflow mid-read.
		{"request_too_large", `{"message":"` + strings.Repeat("a", maxWorkerMessageBytes+1024) + `"}`, http.StatusRequestEntityTooLarge, errIntentRequestTooLarge},
		{"message_empty", `{"message":"  "}`, http.StatusBadRequest, errIntentMessageEmpty},
		{"message_too_long", `{"message":"` + strings.Repeat("가", 4001) + `"}`, http.StatusBadRequest, errIntentMessageTooLong},
		{"bad_request_id", `{"message":"hello","request_id":"has spaces"}`, http.StatusBadRequest, errIntentBadRequestID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(c.body)
			if rec.Code != c.code {
				t.Fatalf("status code = %d, want %d (body %q)", rec.Code, c.code, rec.Body.String())
			}
			if got := errBody(t, rec); got != c.want {
				t.Fatalf("response message = %q, want %q", got, c.want)
			}
			assertEnglishError(t, c.name, errBody(t, rec))
		})
	}
}
