package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"artex/db"
	"artex/norma/agentcore"
	"artex/norma/llm"
	acperm "artex/norma/permission"
	actool "artex/norma/tool"
	"artex/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (segment [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are the penetration-test goal decomposer. Your duty is to identify, from the user's input, the **final results to be achieved** — not to plan attack steps.

**Step one (do this before splitting goals): extract operation constraints**
From the "task goal / task description", identify the operator's explicit rules about **which operations may and may not be performed**, and register them one by one with set_constraints (if the description and goal involve no operation constraints, you may skip extracting them):
- type=deny: forbidden operations (e.g. "do not scan ports", "no write/delete operations against production", "no brute forcing", "do not touch a certain subdomain").
- type=allow: explicitly allowed/limited scope of operations (e.g. "passive recon only", "only target a certain domain").
- A constraint ≠ a goal, and ≠ an attack step: it is a rule about the boundaries of operational behavior.
- **Constraints must be self-contained, with the concrete target written in**: replace **referring words** such as "the current target / current port / current IP / current domain / this site" with the **concrete values** from the task goal/description. Constraints are injected separately into the execution-phase prompts, and out of context a referring word cannot tell who it refers to.
  Example: the target is https://abc.example.net → write "only testing abc.example.net is allowed" rather than "only testing the current target is allowed"; "only test target port 443, do not scan other ports" rather than "only test the current port". If the original text only says "the current target" but the target address is clear, fill in the address.
- **Register only constraints that are explicitly written or emphasized in the goal/description; never fabricate**; when unsure of the type, use deny (more conservative).
- If the goal/description really contains no operation constraints, **do not** call set_constraints.
After registering constraints (if any), proceed with splitting goals below.

**Goal = a final deliverable/verifiable result**

**What is not a goal (never list as sub-goals)**:
- information gathering, reconnaissance, endpoint scanning
- vulnerability analysis and verification process
- attack steps, exploitation techniques
- result verification steps

**Splitting principles**:
- The user describes only one final goal → output one
- There are multiple **mutually independent** final deliverables → list them separately
- Mark vulnclass when it maps to a clear vulnerability class; leave it empty for information-gathering/business-logic goals
- Never fabricate goals the user did not mention

Call set_goals to submit the result.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Additional duty: register the test asset scope**
Besides splitting goals, you must also identify the **explicitly given test asset scope** from the "task goal / task description" and register it with add_task_scope (this task's authorization boundary, and also the denominator of asset test coverage). **Minimal-scope principle: register only the one target the user explicitly named; never enlarge it on your own.**
- The target is a URL or an address with a hostname (e.g. https://xxx.example.com/path, app.example.com) → take its **full hostname**, kind=subdomain, value=the full hostname.
  Example: target https://a1b2c3.lab.example.net/path → kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  **Never** shrink a hostname with subdomains down to the root domain — registering the whole example.com upon seeing xxx.example.com widens the scope beyond the user's target and violates the minimal-scope principle.
- Only when the user gave a **bare root domain without any subdomain** (e.g. just example.com), or explicitly says "the whole site / all subdomains / the entire domain" → use kind=root_domain, value=example.com.
- A plain IP or network range → kind=ip / cidr, value=the IP or CIDR.
- **Do not** register a company scope (company) — the task has just been created and the company usually does not exist in the asset system yet, so it cannot be registered; company-level scope is handled by the later plan phase.
Other rules:
- Register only scope **explicitly written in the goal/description**; never fabricate or infer domains/IPs that were not mentioned.
- In reason, briefly state which sentence the basis comes from, for auditing.
- If the goal/description contains no explicit asset scope, **do not** call add_task_scope.
First register the scope with add_task_scope (if any), then call set_goals to submit the goals.`

// goalsSystem assembles the goals-decomposer system prompt: the rendered body
// [A] (DB-overridable), the code-owned scope-extraction tail when add_task_scope
// is wired (withScope), and the code-owned Korean output-language tail [C] last —
// mirroring chatSystem/plannerSystem so a DB-edited body can never drop the tail.
// DecomposeGoalsWithProvider and the localization test share this one assembly, so
// the langDirective tail can't drift between runtime and test. EngagementDescription
// is intentionally left empty: the task description rides in the user message, not
// the {{.EngagementDescription}} var (see DecomposeGoalsWithProvider).
func goalsSystem(dataDir string, withScope bool) string {
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	if withScope {
		sys += goalsScopeTail
	}
	return sys + langDirective(resolveOutputLang())
}

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (background: target scope / number of flags / rules of engagement, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call: no transcript store is attached, so agentcore
	// does not put a session id on ctx (it only does so when there is a writer; see
	// agentcore.Prompt). But gateways that do prompt caching / sticky routing by a
	// session-id header (opencode zen returns 400 MissingSessionID without
	// x-opencode-session) read exactly this ctx value — without it, "chat works but
	// decomposition gets 400". Attach a stable id explicitly: decomposition requests of
	// the same exploration share it (helps cache hits), and its naming does not clash with
	// planner/worker, so llmrec.parseSession attributes it correctly.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// goalsSystem appends the scope-extraction tail in lockstep (withScope) so the
	// prompt never asks for a tool that isn't present, and it owns the output-language
	// tail last so a DB-edited body can't drop it. Description rides in the user
	// message, NOT the {{.EngagementDescription}} var, so a prompt can't inject it twice.
	withScope := as != nil && taskID > 0
	sys := goalsSystem(dataDir, withScope)
	// set_constraints is always available (does not depend on the asset store): the body
	// already contains the "extract operation constraints before splitting goals" step
	// (wording editable on the agent edit page); here we only need to wire the tool.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	if withScope {
		tools = append(tools, tsx.addTaskScope())
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background information; may include target scope / number of flags / rules of engagement; for reference only — do not fabricate anything it does not mention):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// The 3 steps (extract constraints → register scope → split goals) each need one tool call; allow enough turns so set_goals is not missed before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // uses Provider.Complete when this profile selects non-streaming
		MaxTokens:    maxTokens,    // 0 = send no limit; the server-side default decides
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}
