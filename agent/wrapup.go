package agent

import (
	"strings"

	"github.com/Autumn-27/artex/norma/harness"
)

// Wrap-up (settlement) prompts: when an agent is terminated for [running out of steps (MaxTurns)] or
// [timing out (run_seconds/MaxDuration)], the SDK's settlement phase injects this prompt,
// so the agent first persists what it identified but has not written back, then outputs a one-sentence summary instead of trailing off.
//
// Each agent's wrap-up prompt can be overridden in the admin UI (stored in agents.wrapup_prompt); empty uses the
// built-in default here. Only the [prompt text] is editable; which tools are disabled and the wrap-up's own turn budget are fixed policy in code.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in default wrap-up prompts, keyed by agent key. The worker reuses the historically hard-coded settleWrapUpPrompt
// (defined in worker.go); planner/mainagent each have their own; anything else (custom agents) falls back to the generic one.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults: built-in default turn budget for each agent's wrap-up phase [itself] (overridable by an admin value >0).
// All get 10 turns, so the wrap-up phase has enough steps to persist. Others use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "The step budget for this planning round is about to run out. But only [this round] is ending; the system will wake you again later as the situation changes so you can continue planning. The task itself is not ending, so there is no need to wrap up the whole plan here. Act on the conclusions you have already clearly reached this round so the round is not wasted, but [do not force out intents just to wrap up] (0 intents this round is a perfectly normal outcome). (1) If you have already decided on exploration directions [that should be dispatched right now], submit them together in a single add_intent call (don't sit on what you have already decided; send it now). (2) Mark goals already proven achieved by some finding or fact as met with prove_goal (don't miss any). (3) If you identified a serial exploit chain that must be split into stages, record it with TodoWrite so you can continue dispatching it the next time you wake. When done, end this round immediately and do not output any summary text."

const mainAgentWrapUpDefault = "The step budget is about to run out and this interaction is ending. Do not start any new exploration or operation. **In one sentence of plain text only**, summarize for the user the current progress, the key conclusions, and the recommended next step."

const genericWrapUpDefault = "This run is about to end because its budget is exhausted. First record results you completed but have not yet saved, then **in one sentence of plain text only** summarize what you did and the key conclusions you reached (this one sentence is shown as the result of this run)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        withWrapupLang(resolveWrapup(agentKey)),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// withWrapupLang appends the output-language directive (langDirective) to a wrap-up prompt.
// Unlike system prompts, wrap-up prompts don't get the langDirective tail automatically,
// so it must be appended here for the final user-visible one-sentence summary to follow the chosen display language.
// The wrap-up prose itself is an operating instruction the model reads (kept in English); this tail, at top priority,
// only forces the language of user-visible text. It is harmless for the planner family, which outputs no summary.
// No tail is appended for English: the prose is already English and the system prompt's langDirective
// already governs the output language, so the default path's model input stays minimal.
func withWrapupLang(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	lang := resolveOutputLang()
	if lang == "en" {
		return prompt
	}
	return prompt + langDirective(lang)
}

// ---------- Task-level timeout wrap-up prompts (see the task-level timeout and wrap-up design doc) ----------
//
// These are a [separate set] from the per-run wrap-up prompts: per-run means "this run of yours is out of budget"; task timeout means
// "the whole task's time is up and it is about to end". The semantics are often opposite (especially for the planner: per-run says "don't stop, keep planning",
// task timeout says "time is up, stop planning and make the final judgment"). Configured only for worker/planner.

// WrapupTaskTimeoutOverride / …TurnsOverride: DB overrides for the task-timeout wrap-up prompt and turns
// (wired to agents.task_timeout_wrapup_prompt / _max_turns, worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The whole task has reached its time limit and is about to end** (not this run's budget, but the end of the entire exploration). This is the last chance. (1) Save [everything] you already identified but have not yet recorded: new assets with insert_assets, exploration conclusions and facts with record_fact, confirmed vulnerabilities with report_finding. (2) Do not start any new command or probe. (3) **At the very end, in one sentence of plain text only**, summarize the key conclusions reached for this intent."

const plannerTaskTimeoutDefault = "**The whole task has reached its time limit and is about to end** (not this round, but the entire task). Make the final goal judgment based on [all] facts and findings so far. Mark goals proven achieved by evidence as met with prove_goal (don't miss any). **Do not create any new intents** (intents dispatched now will no longer run). End immediately after the judgment, without outputting any summary text."

// TaskTimeoutWrapupDefault returns an agent's built-in default task-timeout wrap-up prompt (for the admin placeholder / restore default).
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // not configured (mainagent/chat) returns an empty string
}

// resolveTaskTimeoutWrapup: DB override (non-empty) > built-in default. An empty string means the agent has no task-timeout prompt
// (not worker/planner), and the caller should fall back to the per-run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // defaults to the per-run turn budget
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true  → this run is squeezed by the task deadline: wrap-up due to Timeout = task time is up → task-timeout prompt;
//     wrap-up due to MaxTurns = steps ran out first within the squeezed window with minutes left on the task → fall back to the per-run prompt.
//   - clamped=false → the task has plenty of time: both reasons use the per-run prompt (i.e. degrades to wrapupSettlement).
//
// The harness's PromptByReason picks by the [actual] reason at wrap-up time, so there is no build-time mismatch.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := withWrapupLang(resolveWrapup(agentKey))
	st := &harness.Settlement{
		Prompt:        perRun, // fallback (also the value for both reasons when not clamped)
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  withWrapupLang(tt), // task time is up
				harness.ReasonMaxTurns: perRun,             // steps ran out first, task still has time
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
