package server

import "testing"

// Regression test that keeps finding_traffic.go's user-facing HTTP error responses in
// English. The English check reuses assertEnglishError (no Hangul, Han, or kana). The
// paths returning these four messages (findingTrafficAccess, bindFindingTraffic,
// editFindingTraffic) all sit behind the s.m.pg.GetFinding (DB) gate and cannot run
// end to end on a DB-less host, so the constants themselves are pinned (same approach
// as the DB-gated paths of goals_api and notify_api). If anyone reverts these literals
// to Korean or Chinese, this test fails.
//
// readEvidencePreview's errors.New (offset/length validation), the binary-body
// placeholder and the roTool("get_finding_traffic") tool description are agent input
// fed back through tool results and are not asserted here.
func TestFindingTrafficErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"inherited_readonly", errFindingTrafficInheritedReadonly},
		{"select_required", errFindingTrafficSelectRequired},
		{"version_required", errFindingTrafficVersionRequired},
		{"binding_ids_required", errFindingTrafficBindingIDsRequired},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
