package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests that keep the validation error responses returned via writeErr by
// customtool.go's custom tool CRUD and test-run endpoints in English. The English check
// reuses assertEnglishError (no Hangul, Han, or kana) and response-body extraction
// reuses decodeErrorField from the task_categories tests (same package server). The
// actool.Errorf tool results and tool schema descriptions the agent reads are agent
// input and are not covered by this test.

// TestCustomToolErrorConstantsLocalized asserts that all 8 constants exposed via
// writeErr are English. Two of them (errCustomToolKeyExists,
// errCustomToolEditCustomOnly) are reachable only after the handler goes through
// pg.GetTool (DB), so the real HTTP tests below cannot reach them. They are pinned by
// constant assertions, and which branch uses each constant was confirmed by tracing
// the code path (same verification depth as the DB paths of goals_api and task_control).
func TestCustomToolErrorConstantsLocalized(t *testing.T) {
	for _, c := range []struct{ name, msg string }{
		{"keyFormat", errCustomToolKeyFormat},
		{"kindInvalid", errCustomToolKindInvalid},
		{"httpSchemaRequired", errCustomToolHTTPSchemaRequired},
		{"keyExists", errCustomToolKeyExists},
		{"editCustomOnly", errCustomToolEditCustomOnly},
		{"badBody", errCustomToolBadBody},
		{"shellNoExec", errCustomToolShellNoExec},
		{"unknownKindPrefix", errCustomToolUnknownKindPrefix},
	} {
		assertEnglishError(t, "customtool."+c.name, c.msg)
	}
}

// newCustomToolServer builds a Server that passes the pg gate (s.pg) but has no real DB
// connection. &db.DB{} has a nil embedded *sql.DB, but the pointer itself is non-nil, so
// pg() returns it without writing 503. The validation branches below all return
// "before" any real DB call such as pg.GetTool, so the nil DB is never dereferenced
// (confirmed by the code path).
func newCustomToolServer() *Server {
	return &Server{m: &Manager{pg: &db.DB{}}}
}

// TestCustomToolCreateValidationLocalized runs the 3 input-validation paths of
// pgCreateCustomTool over real HTTP and confirms the response body is English. All
// three return before pg.GetTool.
func TestCustomToolCreateValidationLocalized(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"bad-key", `{"key":"BadKey","kind":"command"}`, errCustomToolKeyFormat},
		{"bad-kind", `{"key":"goodkey","kind":"bogus"}`, errCustomToolKindInvalid},
		{"http-no-schema", `{"key":"goodkey","kind":"http"}`, errCustomToolHTTPSchemaRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newCustomToolServer()
			req := httptest.NewRequest(http.MethodPost, "/api/tools/custom", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			s.pgCreateCustomTool(rec, req)
			if rec.Code != 400 && rec.Code != 409 {
				t.Fatalf("expected a validation error status code, got %d (body %q)", rec.Code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("response body mismatch: got %q want %q", got, c.want)
			}
			assertEnglishError(t, "customtool.create."+c.name, got)
		})
	}
}

// TestCustomToolTestRunValidationLocalized runs the 3 input-validation paths of
// pgTestCustomTool over real HTTP and confirms the response body is English. None of
// them calls the DB.
func TestCustomToolTestRunValidationLocalized(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"bad-body", `not-json`, errCustomToolBadBody},
		{"shell", `{"kind":"shell"}`, errCustomToolShellNoExec},
		{"unknown-kind", `{"kind":"bogus"}`, errCustomToolUnknownKindPrefix + "bogus"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newCustomToolServer()
			req := httptest.NewRequest(http.MethodPost, "/api/tools/custom/test", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			s.pgTestCustomTool(rec, req)
			if rec.Code != 400 {
				t.Fatalf("expected status code 400, got %d (body %q)", rec.Code, rec.Body.String())
			}
			got := decodeErrorField(t, rec.Body.Bytes())
			if got != c.want {
				t.Fatalf("response body mismatch: got %q want %q", got, c.want)
			}
			// unknown-kind has the req.Kind ("bogus") suffix appended, so only the constant prefix is checked.
			assertEnglishError(t, "customtool.test."+c.name, errCustomToolUnknownKindPrefix)
		})
	}
}
