package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep findings_groups.go's user-facing messages in English.
// The English check reuses assertEnglishError (no Hangul, Han, or kana).

// TestDeepenFindingBodyTooLargeLocalized confirms over real HTTP that the body-too-large
// (413) response is English. This path returns in the MaxBytesReader decode step,
// before reaching s.m.pg (DB), so it runs end to end even with an empty Server.
func TestDeepenFindingBodyTooLargeLocalized(t *testing.T) {
	s := &Server{}
	body := `{"description":"` + strings.Repeat("a", 33<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/exploration/findings/1/deepen", strings.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	s.deepenFinding(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status code = %d, want %d (body %q)", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
	}
	const want = "The request body is too large"
	if resp.Error != want {
		t.Fatalf("response message = %q, want %q", resp.Error, want)
	}
	assertEnglishError(t, "body_too_large", resp.Error)
}

// TestFindingFollowUpAuditSummaryLocalized asserts that the follow-up intent activity
// summary constant is English. AddFindingFollowUpIntent, which stores this summary,
// needs a DB transaction and cannot run end to end on a DB-less host, so the constant
// itself is asserted. This message is a display-only activity timeline summary,
// separate from the intent payload the agent reads (the user-entered description).
func TestFindingFollowUpAuditSummaryLocalized(t *testing.T) {
	assertEnglishError(t, "follow_up_summary", auditFindingFollowUpSummary)
}
