package server

import "testing"

// Regression test that keeps manager.go's worker-agent count validation error in
// English. The English check reuses assertEnglishError (no Hangul, Han, or kana). The
// SetWorkers(n<=0) branch returns before reaching m.pg.SetSetting, so it can really be
// driven with an empty Manager on a DB-less host. If anyone reverts this literal to
// Korean or Chinese, this test fails.
//
// Call-graph verdict (see the errWorkersPositive constant comment): this message is
// user-facing only, exposed solely through the settings PUT handler (server.go →
// writeErr 400); there is no agent tool (actool) path, so it is not agent input. The
// startup recovery error elsewhere in the same file is an operator startup log and
// out of scope for this test.
func TestSetWorkersErrorLocalized(t *testing.T) {
	err := (&Manager{}).SetWorkers(0)
	if err == nil {
		t.Fatal("a worker-agent count of 0 must be rejected")
	}
	assertEnglishError(t, "set_workers_nonpositive", err.Error())
}
