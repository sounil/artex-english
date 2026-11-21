package server

import (
	"fmt"
	"strings"
	"testing"
)

// Regression test that keeps the "display-only" runtime activity summary constants
// in engine.go, engine_timeout.go and goals.go in English. These summaries appear
// in the dashboard activity transcript but carry no node_id (they are task-level),
// so no agent context ever reads them back — the feedback paths
// (planner.workerOutput, the get_worker_output tool) only pick 'result'/'text'
// activities scoped to an intent (node_id). Summaries that do feed back into the
// agent (the seed intent summary, the default description, etc.) are out of scope
// here. The English check reuses assertEnglishError (no Hangul, Han, or kana;
// intercept_archive_localized_test.go). [[G132]]
func TestRuntimeActivitySummariesLocalized(t *testing.T) {
	// Fixed summaries without format args: must be English as-is.
	plain := map[string]string{
		"goalless_task_done": goallessTaskDoneSummary,
		"goal_breakdown_r0":  goalBreakdownRound0Summary,
		"queued_no_llm":      queuedNoLLMSummary,
		"queued_fifo":        queuedFIFOSummary,
	}
	for label, msg := range plain {
		assertEnglishError(t, label, msg)
	}

	// %d format strings: confirm the format arg survives, the formatted result is
	// English, and the number is actually reflected.
	fmtCases := map[string]string{
		"planner_round":       plannerRoundSummaryFmt,
		"timeout_final_round": timeoutFinalRoundSummaryFmt,
		"queued_conc_limit":   queuedConcurrencyLimitSummaryFmt,
	}
	for label, f := range fmtCases {
		if !strings.Contains(f, "%d") {
			t.Fatalf("%s: format arg %%d is missing: %q", label, f)
		}
		got := fmt.Sprintf(f, 7)
		assertEnglishError(t, label, got)
		if !strings.Contains(got, "7") {
			t.Fatalf("%s: format arg not reflected in the result: %q", label, got)
		}
	}
}
