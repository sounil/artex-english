package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep server.go's user-facing responses in English (the
// source language) (F3b-server.go + F34). The English check reuses
// assertEnglishError (no Hangul, Han, or kana) and response-body extraction reuses
// decodeErrorField from the task_categories tests (same package server).
// Scope: (1) F3b: the 24 writeErr sites + the 2 fmt.Errorf errors that
// validateTaskProfileIDs exposes through writeErr; (2) F34: the 5 user-facing
// outputs that bypass writeErr (chatUnavailableReason's 3 returns + the 2 writeJSON
// errors of testLLM and the web search probe); (3) F34(d): the 3 responses of the
// rule-mode chat fallbackChat (2 acknowledgements + the status summary) and
// recognition of the trigger aliases (intent/hint). Prompts, tool descriptions,
// seed intent summaries, the default title and logs sent to the agent are out of
// scope for this test.

// TestServerErrorConstantsLocalized asserts that every response constant is English.
// Format-string constants (containing %d) are also checked after real formatting,
// confirming the text stays English once values are substituted.
func TestServerErrorConstantsLocalized(t *testing.T) {
	plain := map[string]string{
		"intent_control":        errTaskDeletingIntentControl,
		"intent_rerun":          errTaskDeletingIntentRerun,
		"intent_not_rerunnable": errIntentNotRerunnable,
		"source_invalid":        errCreateTaskSourceInvalid,
		"intercept_rules":       errCreateTaskInterceptRules,
		"category_invalid":      errCreateTaskCategoryInvalid,
		"company_invalid":       errCreateTaskCompanyInvalid,
		"llm_profile_invalid":   errLLMProfileInvalid,
		"asset_store_disabled":  errAssetStoreDisabled,
		"asset_id_required":     errAssetIDRequired,
		"python_not_detected":   errPythonNotDetected,
		"notify_base_url":       errNotifyBaseURLScheme,
		"notify_digest":         errNotifyDigestRange,
		"new_session":           errTaskDeletingNewSession,
		"new_message":           errTaskDeletingNewMessage,
		"main_agent_busy":       errMainAgentBusy,
		// F34: user-facing responses that bypass writeErr
		"chat_no_profile":       errChatNoLLMProfile,
		"chat_no_active":        errChatNoActiveLLMProfile,
		"chat_not_ready":        errChatLLMNotReady,
		"llm_test_no_api_key":   errLLMTestNoAPIKey,
		"web_search_no_results": errWebSearchProbeNoResults,
		// F34(d): the two acknowledgements of rule-mode chat fallbackChat (the input text is appended)
		"fallback_intent": fallbackIntentInjected,
		"fallback_hint":   fallbackHintRecorded,
	}
	for label, msg := range plain {
		assertEnglishError(t, label, msg)
	}

	formatted := map[string]string{
		"source_limit":     fmt.Sprintf(errCreateTaskSourceLimit, 8),
		"source_not_found": fmt.Sprintf(errCreateTaskSourceNotFound, 999),
		"company_limit":    fmt.Sprintf(errCreateTaskCompanyLimit, 32),
		"llm_profile_404":  fmt.Sprintf(errLLMProfileNotFound, 7),
		// F34(d): rule-mode status summary (asset, pending intent and confirmed vulnerability counts substituted)
		"fallback_status": fmt.Sprintf(fallbackChatStatus, 3, 2, 1),
	}
	for label, msg := range formatted {
		assertEnglishError(t, label, msg)
	}
}

// TestServerHandlersResponsesLocalized checks handler paths that finish without
// touching the DB all the way to the actual HTTP response body, confirming the
// constants really reach the response (reverting a constant to Korean or Chinese
// breaks this test = adversarial non-vacuity). Paths that need the DB, engine or
// asset store (invalid category/company, missing LLM profile, Python not detected,
// notification settings, main agent busy) are pinned by the constant assertions above.
func TestServerHandlersResponsesLocalized(t *testing.T) {
	// Server with the delete barrier raised: beginTaskOperation / IsDeleting report "deleting".
	deletingServer := func() *Server {
		s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
		s.engine.deleting.Store("t1", true)
		return s
	}
	// Server without the barrier: proceeds to input validation.
	liveServer := func() *Server {
		return &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
	}

	cases := []struct {
		name    string
		req     func() *http.Request
		handler func(*Server) http.HandlerFunc
		server  func() *Server
		code    int
		want    string
	}{
		{
			name: "controlIntent/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/5/control", strings.NewReader(`{"action":"pause"}`))
				r.SetPathValue("id", "t1")
				r.SetPathValue("iid", "5")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.controlIntent },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentControl,
		},
		{
			name: "rerunIntent/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/5/rerun", nil)
				r.SetPathValue("id", "t1")
				r.SetPathValue("iid", "5")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.rerunIntent },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentRerun,
		},
		{
			name: "rerunBlocked/task-deleting",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/intents/rerun-blocked", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.rerunBlocked },
			server:  deletingServer,
			code:    http.StatusConflict,
			want:    errTaskDeletingIntentRerun,
		},
		{
			name: "createTask/source-limit",
			req: func() *http.Request {
				// MaxTaskSourceCount=8 → 9 exceeds the limit (returns before the loop).
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"goal","source_task_ids":["1","2","3","4","5","6","7","8","9"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskSourceLimit, 8),
		},
		{
			name: "createTask/source-invalid",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"goal","source_task_ids":["abc"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errCreateTaskSourceInvalid,
		},
		{
			name: "createTask/source-not-found",
			req: func() *http.Request {
				// 999 is not in the tasks map → not found (map lookup, no DB access).
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"goal","source_task_ids":["999"]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskSourceNotFound, 999),
		},
		{
			name: "createTask/company-limit",
			req: func() *http.Request {
				// MaxTaskCompanyCount=32 → 33 fails in NormalizeTaskCompanyIDs (a pure function).
				ids := make([]string, 33)
				for i := range ids {
					ids[i] = fmt.Sprintf("%d", i+1)
				}
				body := `{"goal":"goal","company_ids":[` + strings.Join(ids, ",") + `]}`
				return httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    fmt.Sprintf(errCreateTaskCompanyLimit, 32),
		},
		{
			name: "createTask/llm-profile-invalid",
			req: func() *http.Request {
				// llm_profile_ids:[0] → validateTaskProfileIDs returns before loadProfileConfig (DB).
				r := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(
					`{"goal":"goal","llm_profile_ids":[0]}`))
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.createTask },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errLLMProfileInvalid,
		},
		{
			name: "taskCoverageGraph/asset-store-disabled",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/coverage-graph", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.taskCoverageGraph },
			server:  liveServer, // Manager.assets==nil → Assets()==nil → 503
			code:    http.StatusServiceUnavailable,
			want:    errAssetStoreDisabled,
		},
		{
			name: "taskAssetRefs/asset-id-required",
			req: func() *http.Request {
				// no asset_id query → ParseInt("")=0 → <=0 → 400 (before calling Assets()).
				r := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/asset-refs", nil)
				r.SetPathValue("id", "t1")
				return r
			},
			handler: func(s *Server) http.HandlerFunc { return s.taskAssetRefs },
			server:  liveServer,
			code:    http.StatusBadRequest,
			want:    errAssetIDRequired,
		},
		{
			// F34(b): with profile_id empty (= no DB lookup) and api_key empty, it returns
			// writeJSON{ok:false,error} before TestConnection (status 200). liveServer's
			// llmCfg.APIKey is also "", so there is no global fallback key either.
			name: "testLLM/no-api-key",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/api/llm/test", strings.NewReader(`{}`))
			},
			handler: func(s *Server) http.HandlerFunc { return s.testLLM },
			server:  liveServer,
			code:    http.StatusOK,
			want:    errLLMTestNoAPIKey,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.server()
			rec := httptest.NewRecorder()
			c.handler(s)(rec, c.req())
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

// TestChatUnavailableReasonLocalized confirms that the reason F34(a)'s
// chatUnavailableReason() actually returns to the user is wired to the English
// constant. With pg nil (no DB in the test) it falls straight into the "not ready"
// path, so this asserts that return is errChatLLMNotReady (reverting the constant
// to Korean or Chinese breaks it = adversarial non-vacuity). The other two reasons
// (no profile, no active profile) need pg (DB) and are pinned by the constant
// assertions above.
func TestChatUnavailableReasonLocalized(t *testing.T) {
	s := &Server{m: &Manager{}} // m.pg == nil → third return path
	got := s.chatUnavailableReason()
	if got != errChatLLMNotReady {
		t.Fatalf("chatUnavailableReason() = %q, want %q", got, errChatLLMNotReady)
	}
	assertEnglishError(t, "chat_not_ready", got)
}

// TestFallbackCommandParsing confirms that rule-mode chat trigger parsing
// (fallbackCommand) accepts the English triggers as well as the legacy Korean
// (의도/힌트) and Chinese (意图/提示) aliases, and returns the argument with only the
// keyword stripped. It is pure input parsing that touches neither Store nor DB, so it
// runs without a DB. Removing any alias breaks its case (non-vacuity).
func TestFallbackCommandParsing(t *testing.T) {
	cases := []struct {
		name, in, cmd, text string
	}{
		{"korean-intent", "의도 check the SQLi injection point first", "intent", "check the SQLi injection point first"}, // legacy: Korean trigger alias
		{"korean-hint", "힌트 look at the login form first", "hint", "look at the login form first"},                     // legacy: Korean trigger alias
		{"chinese-intent", "意图 scan ports", "intent", "scan ports"},                                                    // legacy: Chinese trigger alias
		{"chinese-hint", "提示 check login", "hint", "check login"},                                                      // legacy: Chinese trigger alias
		{"english-intent", "intent scan ports", "intent", "scan ports"},
		{"english-hint", "hint try admin:admin", "hint", "try admin:admin"},
		{"status-plain", "what is the current status", "", "what is the current status"},
		{"status-empty", "   ", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, text := fallbackCommand(c.in)
			if cmd != c.cmd || text != c.text {
				t.Fatalf("fallbackCommand(%q) = (%q, %q), want (%q, %q)", c.in, cmd, text, c.cmd, c.text)
			}
		})
	}
}
