package server

import "testing"

// TestReporterSeedLabelsLocalized is a regression guard that keeps the display-only
// labels of the seeded reporter agent (reporterAgentName, reporterAgentDescription)
// in English. Both strings are rendered only in the system/agents UI via `agentDTO`
// (server_mgmt.go); they are pure UI labels that never enter any agent system prompt
// (renderSystem assembles only key → section [A] agent.ReporterDefaultPrompt) nor
// model-based agent selection (the reporter trigger is the deterministic OnToolCall
// on the report_finding tool call). The reporter's prompt body
// (agent.ReporterDefaultPrompt) and trigger message (reporterToolCallMessage) are
// model input and are not covered here. It reuses assertEnglishError (no Hangul,
// Han, or kana; intercept_archive_localized_test.go). [[F35]] [[G133]]
func TestReporterSeedLabelsLocalized(t *testing.T) {
	assertEnglishError(t, "reporter_name", reporterAgentName)
	assertEnglishError(t, "reporter_description", reporterAgentDescription)
}
