package agent

import (
	"strings"
	"testing"
	"unicode"
)

// Wrap-up / settlement prompts are English source text.
//
// These constants are injected by the settlement phase when a run or task hits its step/time
// budget, and directly instruct the final summary shown to the user. So (1) they must be
// written in English with no Hangul, Han or kana left over, and (2) tool names and
// instructions like "one sentence of plain text" must be preserved.
//
// The DB seed leaves wrapup_prompt / task_timeout_wrapup_prompt empty (the builtin agent
// INSERT in db/db.go does not fill these columns), and empty values fall back to these
// constants. So these constants are the only source of the wrap-up wording.
func TestWrapupPromptsAreEnglish(t *testing.T) {
	all := map[string]string{
		"settleWrapUpPrompt":        settleWrapUpPrompt,
		"plannerWrapUpDefault":      plannerWrapUpDefault,
		"mainAgentWrapUpDefault":    mainAgentWrapUpDefault,
		"genericWrapUpDefault":      genericWrapUpDefault,
		"workerTaskTimeoutDefault":  workerTaskTimeoutDefault,
		"plannerTaskTimeoutDefault": plannerTaskTimeoutDefault,
	}

	hasScript := func(s string, tables ...*unicode.RangeTable) bool {
		for _, r := range s {
			if unicode.In(r, tables...) {
				return true
			}
		}
		return false
	}

	for name, p := range all {
		if hasScript(p, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			t.Errorf("%s: CJK text left over, translation incomplete: %q", name, p)
		}
	}

	// Tool names are identifiers and must be preserved untranslated.
	// The worker family (per-run and task-timeout) keeps the instructions to write conclusions
	// with record_fact, vulns with report_finding, and to finish with a one-sentence plain-text summary.
	mustContain := func(name, p string, subs ...string) {
		for _, s := range subs {
			if !strings.Contains(p, s) {
				t.Errorf("%s: instruction %q should be preserved but is missing", name, s)
			}
		}
	}
	mustContain("settleWrapUpPrompt", settleWrapUpPrompt,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("workerTaskTimeoutDefault", workerTaskTimeoutDefault,
		"insert_assets", "record_fact", "report_finding", "one sentence", "plain text")
	mustContain("genericWrapUpDefault", genericWrapUpDefault, "one sentence", "plain text")
	mustContain("mainAgentWrapUpDefault", mainAgentWrapUpDefault, "one sentence", "plain text")
	// The planner outputs no summary sentence (it only judges and ends) and keeps the intent/goal/todo tools.
	mustContain("plannerWrapUpDefault", plannerWrapUpDefault, "add_intent", "prove_goal", "TodoWrite")
	mustContain("plannerTaskTimeoutDefault", plannerTaskTimeoutDefault, "prove_goal")
}

// per-run and task-timeout must differ in meaning (especially for the planner): per-run is "only this
// round ends" and task-timeout is "the whole task ends". The constant mapping must not get mixed up.
func TestWrapupDefaultsRouting(t *testing.T) {
	if WrapupDefault("worker") != settleWrapUpPrompt {
		t.Error("worker per-run default is not settleWrapUpPrompt")
	}
	if WrapupDefault("planner") != plannerWrapUpDefault {
		t.Error("planner per-run default is not plannerWrapUpDefault")
	}
	if WrapupDefault("mainagent") != mainAgentWrapUpDefault {
		t.Error("mainagent per-run default is not mainAgentWrapUpDefault")
	}
	// Unregistered keys (custom agents) fall back to generic.
	if WrapupDefault("unknown-agent") != genericWrapUpDefault {
		t.Error("unregistered key does not fall back to genericWrapUpDefault")
	}
	// task-timeout exists only for worker/planner; others get an empty string (the caller falls back to per-run).
	if TaskTimeoutWrapupDefault("worker") != workerTaskTimeoutDefault {
		t.Error("worker task-timeout default is not workerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("planner") != plannerTaskTimeoutDefault {
		t.Error("planner task-timeout default is not plannerTaskTimeoutDefault")
	}
	if TaskTimeoutWrapupDefault("mainagent") != "" {
		t.Error("mainagent must have no task-timeout wording (empty string)")
	}
	// If the per-run and task-timeout wording are identical, the distinction in meaning is lost.
	if workerTaskTimeoutDefault == settleWrapUpPrompt {
		t.Error("worker per-run and task-timeout wording are identical")
	}
	if plannerTaskTimeoutDefault == plannerWrapUpDefault {
		t.Error("planner per-run and task-timeout wording are identical")
	}
}
