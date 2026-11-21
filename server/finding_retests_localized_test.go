package server

import "testing"

// Regression test that keeps finding_retests.go's user-facing HTTP error responses in
// English. The English check reuses assertEnglishError (no Hangul, Han, or kana). The
// startFindingRetest handler that returns these four messages passes the s.pg(w) (DB)
// gate first, so it cannot run end to end on a DB-less host; the constants themselves
// are pinned instead (same approach as the DB-gated paths of finding_traffic,
// goals_api and notify_api). If anyone reverts these literals to Korean or Chinese,
// this test fails.
//
// Tool descriptions (get_finding_retest_context, record_finding_retest_result),
// parameter descriptions, actool.Errorf (the run-not-linked notice) and
// seedFindingRetester's DB-seeded agent name/profile/note are agent input or seed data
// and are not asserted here (see the constant block comment in finding_retests.go).
func TestFindingRetestErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"notes_too_long", errFindingRetestNotesTooLong},
		{"agent_missing", errFindingRetestAgentMissing},
		{"tool_required", errFindingRetestToolRequired},
		{"service_stopping", errFindingRetestServiceStopping},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
