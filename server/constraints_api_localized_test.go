package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep the error responses of constraints_api.go's constraint
// management API in English. The English check reuses assertEnglishError (no Hangul,
// Han, or kana) and response-body extraction reuses decodeErrorField from the
// task_categories tests (same package server).

// TestConstraintErrorConstantsLocalized asserts that all 5 response constants are English.
func TestConstraintErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"task_deleting_add":    errConstraintTaskDeletingAdd,
		"task_deleting_edit":   errConstraintTaskDeletingEdit,
		"task_deleting_delete": errConstraintTaskDeletingDelete,
		"text_empty":           errConstraintTextEmpty,
		"kind_invalid":         errConstraintKindInvalid,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestConstraintHandlersResponsesLocalized checks handler paths that finish without
// touching the DB all the way to the real HTTP response body, confirming the constants
// actually reach the response. All three handlers go s.m.Task (map lookup) →
// s.engine.beginTaskOperation (sync.Map-based delete barrier) → request body/field
// validation, so one task in the tasks map plus an empty Engine runs them without
// Store or DB.
func TestConstraintHandlersResponsesLocalized(t *testing.T) {
	// Server with the deleting barrier raised: beginTaskOperation returns false and yields 409.
	deletingServer := func() *Server {
		s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
		s.engine.deleting.Store("t1", true)
		return s
	}
	// Server without the barrier: beginTaskOperation returns true and proceeds to input validation.
	liveServer := func() *Server {
		return &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
	}

	newReq := func(body, cid string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/constraints", strings.NewReader(body))
		req.SetPathValue("id", "t1")
		if cid != "" {
			req.SetPathValue("cid", cid)
		}
		return req
	}

	cases := []struct {
		name    string
		handler func(*Server) http.HandlerFunc
		server  func() *Server
		body    string
		cid     string
		code    int
		want    string
	}{
		{
			name:    "add/task-deleting",
			handler: func(s *Server) http.HandlerFunc { return s.addConstraint },
			server:  deletingServer,
			body:    `{"text":"test only the current port","kind":"allow"}`,
			code:    http.StatusConflict,
			want:    errConstraintTaskDeletingAdd,
		},
		{
			name:    "add/text-empty",
			handler: func(s *Server) http.HandlerFunc { return s.addConstraint },
			server:  liveServer,
			body:    `{"text":"   ","kind":"allow"}`,
			code:    http.StatusBadRequest,
			want:    errConstraintTextEmpty,
		},
		{
			name:    "add/kind-invalid",
			handler: func(s *Server) http.HandlerFunc { return s.addConstraint },
			server:  liveServer,
			body:    `{"text":"do not scan other ports","kind":"maybe"}`,
			code:    http.StatusBadRequest,
			want:    errConstraintKindInvalid,
		},
		{
			name:    "edit/task-deleting",
			handler: func(s *Server) http.HandlerFunc { return s.editConstraint },
			server:  deletingServer,
			body:    `{"text":"updated constraint","kind":"deny"}`,
			cid:     "5",
			code:    http.StatusConflict,
			want:    errConstraintTaskDeletingEdit,
		},
		{
			name:    "edit/text-empty",
			handler: func(s *Server) http.HandlerFunc { return s.editConstraint },
			server:  liveServer,
			body:    `{"text":"  ","kind":"deny"}`,
			cid:     "5",
			code:    http.StatusBadRequest,
			want:    errConstraintTextEmpty,
		},
		{
			name:    "edit/kind-invalid",
			handler: func(s *Server) http.HandlerFunc { return s.editConstraint },
			server:  liveServer,
			body:    `{"text":"updated constraint","kind":"nope"}`,
			cid:     "5",
			code:    http.StatusBadRequest,
			want:    errConstraintKindInvalid,
		},
		{
			name:    "delete/task-deleting",
			handler: func(s *Server) http.HandlerFunc { return s.deleteConstraint },
			server:  deletingServer,
			body:    ``,
			cid:     "5",
			code:    http.StatusConflict,
			want:    errConstraintTaskDeletingDelete,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.server()
			rec := httptest.NewRecorder()
			c.handler(s)(rec, newReq(c.body, c.cid))
			if rec.Code != c.code {
				t.Fatalf("status code = %d, want %d (body %q)", rec.Code, c.code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("response message = %q, want %q", got, c.want)
			}
			assertEnglishError(t, c.name, got)
		})
	}
}
