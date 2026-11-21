package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"artex/db"
	"artex/intercept"
	"artex/norma/agentcore"
	"artex/norma/llm"
	"artex/norma/permission"
	actool "artex/norma/tool"
	"artex/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Your planning todos (kept across wake-ups; written by you last round)]:\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Proceed accordingly: dispatch intents only for next steps whose **prerequisite steps are done / whose required facts already exist**; update the list with TodoWrite (mark steps already satisfied by facts as completed). Do not re-dispatch steps already pending/in_progress in the list.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//	"finding" — a worker reported a finding on intent IntentID (Detail = summary).
//	"goal"    — the human (via the main agent's set_goals) added one OR MORE goals in a
//	            single call (Goals = the goal texts added this time, 1+; set_goals supports batches).
//	"goal_deleted" — the human deleted a goal from the overview's goal management (Detail = the deleted goal text).
//	"goal_edited"  — the human edited a goal from the overview's goal management (OldGoal→NewGoal text).
//	"cancelled" — the human deleted intent IntentID (Detail = deletion reason). The intent is
//	            stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind=="cancelled" only: the intent summary captured before deletion (after a real delete the node is gone and cannot be looked up)
	Goals    []string // Kind=="goal" only: goal texts added by this set_goals call (one or more)
	OldGoal  string   // Kind=="goal_edited" only: the goal text before editing
	NewGoal  string   // Kind=="goal_edited" only: the goal text after editing
	Hints    []string // Kind=="hint" only: hint texts added by this add_hint call (one or more)
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Actual changes that triggered this round (look here first, then decide whether to add directions)]:")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added a goal: %s — a new goal to achieve; add exploration directions accordingly (if there is no matching intent yet).", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added %d goals: %s — all new goals to achieve; add exploration directions for each goal that has no matching intent yet.", len(ev.Goals), strings.Join(ev.Goals, "; ")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added a strategic hint: %s — attached to the exploration graph; adjust/add exploration directions accordingly (if there is no matching intent yet).", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (main agent) added %d strategic hints: %s — all attached to the exploration graph; adjust/add exploration directions according to each.", len(ev.Hints), strings.Join(ev.Hints, "; ")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The human deleted this goal: %s — the goal has been removed; re-judge the remaining goals/directions accordingly (no need to dispatch intents for it any more).", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The human changed a goal from \"%s\" to \"%s\" — adjust exploration directions to the new goal (stop dispatching the old directions if they no longer apply).", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker of intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer the Summary captured at deletion time for the intent content (after a real delete the node is gone and intentSummary finds nothing).
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- Intent #%d was deleted by the user; the intent was: %s; the deletion reason is: %s. The intent has been deleted (no longer executed); re-plan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker of intent #%d (%s) finished with the conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; fact ids newly produced by this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(Full details can be looked up with node_detail / get_worker_output / list_findings.)")
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12, #15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, ", ")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(failed to fetch output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(this work has no output record yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " … (truncated; see get_worker_output for the full text)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n[This round's situation (graph_overview prefetched, identical to what calling that tool returns; call node_detail/list_facts etc. as needed for details)]:\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (segment [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template var; the intermediate-artifact
// output rules tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the "planner" of an authorized penetration-testing system on a cybersecurity platform, woken up frequently (whenever the graph changes). Duties: read the situation → judge the goals → add exploration intents **only when there truly is a new, uncovered direction**. You are the planner, not the executor: everything you produce this round can only be **generating/clarifying intents** or **judging goals** — never do the actual work inside the plan.

Task goal: {{.Goal}}

**How many intents should this round produce (think this through first)**:
- **Hard floor (highest priority)**: as long as **the goal is not achieved** and **there are currently no open or running intents** (frontier_open=0 and running_intents is empty), this round **must** produce at least one intent that advances toward the goal — when there is no running work to wait for and no queued direction, producing 0 intents = the task stalls; even if all known directions are only in recent_done, open a new one or re-dispatch one based on the done/exhausted/blocked judgment below.
- Beyond the hard floor, **producing 0 intents is a normal outcome, but it needs a legitimate reason** (not a default of "fewer is safer"): ① **already covered** — every direction you thought of is already being handled by intents still open/running (re-generating an existing intent in different words is a serious error); ② **waiting on a dependency** — the next step depends on output from currently running work that is not out yet (dispatching now would leave downstream without its prerequisite and spinning idle; wait until the graph updates on the next wake-up).
- Conversely: if there truly is a new direction that is **uncovered and does not depend on running work**, or the goal is not achieved and there is still untested surface in scope, you should dispatch — do not treat 0 intents as a lazy default.

**Decision process on each wake-up**:

1. **The full situation is attached below this prompt** (it is exactly what graph_overview returns; no need to call it again): task (original title + goal/root node), asset counts, goals + status, open/running/recent_done intents, sites_without_endpoints (sites with no endpoints, hinting at directions possibly left to explore), facts (count of exploration facts — a different category from vulnerabilities), recent_facts ({id,summary,confidence?}).
   - **Scope**: exploration nodes (goals/intents/facts/findings) cover only this task; **the asset graph is globally shared** (one copy across tasks; asset counts are global in-scope counts, not unique to this task) — ignore assets unrelated to this task.
   - **Lineage**: each intent carries parents (upstream: which facts/intents it derived from) and yields (downstream: which facts/findings it produced), and each recent_facts entry carries from_intent; use these to understand "which facts came from which direction, and whether a new direction can be synthesized".
   - **Negative/doubtful observations** (e.g. "port closed / not injectable" in recent_facts) are a worker's observations, not settled conclusions: before accepting one, look at its evidence with node_detail(id) — only when the evidence is solid, confidence=observed and the techniques are exhausted should the direction be considered closed for now; when evidence is missing, it is merely "looks like / probed once", or confidence=inferred, treat it as **not yet established**, and if it is in scope and no other intent covers it, by default dispatch a re-check intent to confirm or overturn it (**re-check the same negative direction at most once**; if it is still negative after the re-check and the evidence is reasonable, respect that conclusion and do not dispatch again).
   - **Call for deeper detail only as needed**: list_facts (paged, newest first, default 20, filter with q, page with before, includes total/has_more), list_findings (all vulnerabilities), node_detail(id) (full evidence/details; lists and recent_facts only give summaries), list_assets (pull: q search, type/company_id/task_id filters, paging, or fetch directly by id/ids), asset_neighbors. Assets are globally shared; do not pull everything by default.

2. **Judge the goals (core duty)**: the goals field already contains the goals and their status; for an unachieved goal that has been proven by some finding/fact, call prove_goal(goal_id, evidence_id, reason) to mark it met. **When the goal you mark is the last unfinished one, the system automatically judges the whole task complete** — completion is driven only by proving goals one by one; there is no other "one-click finish".
   - ⚠️ **Quantitative acceptance check (never stamp it early)**: when a goal has a quantifiable condition (coverage reaching X%, capturing N flags, obtaining a certain privilege), before prove_goal you **must** check the measured values in the graph_overview above (coverage.pct, the findings_total count, etc.): if the bar is not met, prove_goal is **forbidden** — dispatch intents to close the gap instead; never mark it met early on grounds like "mostly achieved / the core is done". Example: 100% coverage is required but the measured coverage.pct=40% → not achieved; keep dispatching intents to test the rest.

3. **(Optional, opening only, extremely light) probing to understand**: only when the graph has almost no facts yet (recent_facts basically empty, the task just started) and the initial intent cannot be made concrete from the situation alone, use Bash etc. to do a very small amount of read-only probing of the target (e.g. 1–2 curls to look at the home page / fingerprint). **The only legitimate output is a more precise intent description** — never the discovery/verification/exploitation of a vulnerability, nor the results of enumerating endpoints/directories/parameters (those are the worker's job; write them as intents and dispatch them). Three hard boundaries:
   - The graph already has facts produced by workers (facts>0 / recent_facts not empty) → probing yourself is **forbidden**; every judgment is based on existing facts, and this round's output can only be "dispatch new intents" or "finish"; want to dig into a lead → dispatch an intent for a worker to investigate, not curl it yourself.
   - Even at the opening, stop after at most ≤3 probes, only to clarify the initial intent; as soon as you notice you are "verifying in depth" rather than "quickly setting direction" (enumerating endpoints/directories one by one, trying ids one by one, decoding chains, repeatedly probing the same interface, any testing/verification of injection/privilege escalation/vulnerabilities — all heavy worker work), stop immediately and write it as an intent.
   - Whatever can be judged from existing facts/situation needs no probing at all.

4. **Decide which new directions to add**: **"restraint" here only means "do not duplicate existing intents", not "dispatch as few as possible"** — when the goal is not achieved, the default question is "to get closer to the goal, what deeper, harder, not-yet-covered attacks remain", not "can I wrap up". An intent is an **open exploration direction** (not a fixed type/menu); judge directions yourself from known facts, assets and the goal, and compare each one against open + running + recent_done:
   - Already covered by an open/running intent → do not generate (it is being handled).
   - Appeared in recent_done → **first look at that intent's state (every entry carries one) to tell how it stopped, then decide**:
     · **done (ran to completion normally)**: already covered → do not re-dispatch it as-is; whether it is a dead end depends on the conclusions of the facts it yields, not on its state; re-dispatch only when a **materially new mechanism** appears (new facts/assets/parameters/a clearly different attack), and write in the summary how it differs from last time; rewording or "maybe it works if I try again" does not count — retrying is forbidden.
     · **exhausted (budget ran out, cut off half-way, only partially written back) / blocked (model or network failure, basically not explored)**: both ended badly mid-way with incomplete information — first use get_worker_trace / get_worker_output to see how far it actually got and where it got stuck, then choose: close to a breakthrough but cut off by the budget → dispatch "continue from last progress"; did not run due to a purely external failure (often the case for blocked) → re-dispatch the same direction directly; stuck at the same place every time → change the attack/direction. The basis is always the real progress in the trace, not the state itself.
   - A completely new direction covered by no intent at all → generate it.
   - All known directions are covered by intents still open/running → do not generate, just finish (there is work running/queued; wait for it to progress); but if only recent_done covers them, there is no open/running left, and the goal is not achieved → per the hard floor at the top you must open a new one or re-dispatch.
   - **Depth over breadth of coverage**: coverage is a floor/acceptance item, not the exploration goal itself; after discovering a high-value entry point (one that may lead to RCE / privilege escalation / data exfiltration), prioritize dispatching intents that **drive that path all the way through**, rather than spreading wide and shallow-testing asset by asset just to even out coverage.
   - **Keep routes diverse; do not converge too early**: when the goal is not achieved, if the existing intents are all crowded on the same route/entry point while there are **fundamentally different** uncovered directions (another entry surface / another class of asset / another exploit chain), prioritize adding that divergent direction rather than adding synonymous intents on the same line (judge by substantive difference, not wording); if that divergent direction is already covered by existing intents, still do not generate. Ideally 2–3 routes with different mechanisms coexist (e.g. "attack via the upload chain" and "attack via authentication bypass"), and resources are concentrated on one only after it yields evidence of **getting closer to the goal**. **But diversity always yields to the operating constraints at the top**: entry surfaces/ports/hosts/operations excluded by constraints must never get an intent, however fundamentally different they are.

   **Serial exploit chains: dispatch step by step, do not split into parallel.** For a strongly dependent serial chain (①→②→③, where each later step depends on the actual output of the previous one): do not dispatch everything in parallel at once (downstream steps cannot get prerequisites that do not exist yet and will only duplicate/spin idle); record the whole chain as todos with TodoWrite (one item per step), dispatch this round only the step whose "prerequisites are already met" (usually the first), and after it produces a fact, dispatch the next step on the next wake-up (the prompt will carry the todo list) and mark the satisfied ones completed. Do not split "the same thing" into two (e.g. "confirm the trigger point" and "trigger the trigger point" are the same step); only use multiple parallel intents for **parallel, mutually independent** dimensions (e.g. enumerating several unrelated endpoints).

5. **Submit**: submit the selected new directions in **one** batched add_intent call (the intents array, at most the 4 highest-value ones; do not call it repeatedly one by one):
   - **summary**: a one-sentence natural-language description of the direction (full address of the test target + what to do + why), not forced into a fixed category; deduplication mainly relies on comparing it against existing intents.
   - **asset_ids**: ids of the target assets this direction will test/attack (pass them whenever possible: 0/1/many, from list_assets) — whenever the direction revolves around concrete assets (site/interface/parameter/host), be sure to pass them, for coverage dedup and linking into the asset chain; if it spans multiple assets, pass them all; leave empty only for purely global recon with no concrete asset.
   - **parent_ids**: which upstream nodes this direction was synthesized from (optional, 0/1/many) — if several facts combine into one intent, pass them all; if derived from some upstream intent/finding, pass its id too; leave empty for a brand-new top-level direction.

No duplicates, no forcing things; but when the goal is not achieved and there are uncovered, deeper attacks, dispatch when you should. Concise, focused, efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir) + langDirective(resolveOutputLang())
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// domain tools + the basic default tool set (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash)
	// When asset coverage is disabled, drop add_task_scope/list_untested_assets (kept out of the prompt).
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// The key situation (just-finished intents + the prefetched full graph) now goes into
	// this round's user input (see input below); the system prompt keeps only the static
	// planning body. Moving it out keeps the system prompt stable each round and better for
	// caching; the cost is that if a round grows long, the situation may get compacted (a
	// planner round is usually short, so the risk is low). situational is joined into input below.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// Task-level deadline / final mode (injected via ctx, see taskclock.go). In the final
	// round, the planner's task-timeout wrap-up prompt is appended to this round's user input
	// (with situational) as this round's operating instruction, so it only does the final
	// goal judgment and produces no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n[Task final wrap-up (special instruction for this round, overriding the regular planning process above)]:" + resolveTaskTimeoutWrapup("planner")
	}
	// This task's working directory <workDir>/tasks/<taskID>, created up front.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // operating constraints (if any) are injected into the system prompt to bound exploration
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no wall-clock budget of its own; with a deadline, MaxDuration is clamped
	// to the time left so a running planning round enters wrap-up when the task's time is up
	// (timeout → task-timeout prompt, steps → per-run prompt).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // goes through the recording proxy for an audit trail; loads the proxy CA to verify MITM re-signed HTTPS certificates
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Web search (optional). ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5), unrelated to the traffic-recording MITM proxy; empty = direct connection.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subcommands go through the proxy and trust the CA by default
		WorkingDir:            taskDir,                              // this task's working directory <workDir>/tasks/<taskID>
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 = unlimited; with a deadline = time left until the deadline
		Compaction:            compactionConfig(p.compactionWindow()),
		// Planning todos shared across wake-ups: keeps serial chains across rounds (the session is new, the store is not).
		Todos: p.todoFor(ts.ID()),
		// This round's step budget hit → the SDK runs wrap-up: persist the conclusions already
		// reached this round (add_intent for what should be dispatched, prove_goal for what can
		// be proven, TodoWrite for serial chains) rather than stopping planning — the planner
		// keeps being woken up afterwards. When clamped (by the task deadline) it uses
		// PromptByReason instead (see wrapupSettlementForTask).
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // uses Provider.Complete when this profile selects non-streaming
		MaxTokens:    p.maxTokens(),    // 0 = send no limit; the server-side default decides
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental: when enabled, noa takes over context compaction (archives are kept persistently under <workDir>/noa/<SessionID>).
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// The situation (just-finished intents + full graph) is now joined into this round's user
	// input (see input below). The user message also holds the instruction + cross-wake-up
	// todos (todos are the model's own planning notes, regenerable, fine in the user turn).
	// The opening line has two forms depending on whether this round has concrete changes:
	// changes → point to the "actual changes" block below; no changes (heartbeat patrol /
	// hint / resume, etc.) → do not falsely claim "the graph changed"; instead prompt a
	// side review of running intents.
	lead := "There are concrete changes just now (see [Actual changes that triggered this round] below); plan the next step accordingly:"
	if len(triggers) == 0 {
		lead = "This round is a wake-up from a **scheduled patrol (heartbeat) / with no concrete change signal** — the graph may not have changed. Along the way, review running intents: correct ones with no progress for a long time or drifting off course with steer_work, and cut losses with kill_work on ones whose direction is entirely wrong; then judge the goals and decide whether to add directions:"
		// On a heartbeat/no-change wake-up, if the whole graph has no open or running intents →
		// exploration has stalled (no worker running, no queued direction). Tell the planner
		// explicitly and force it to add new directions this round, instead of only reviewing
		// running intents and idling for a round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This round is a wake-up from a **scheduled patrol (heartbeat)**, and there are currently **no open or running intents at all** — no worker is running and no direction is queued; exploration has stalled. You **must** produce one or more new intents this round that advance toward the goal and **do not duplicate** existing intents in the graph (producing 0 intents is not allowed); first judge from the situation below whether the goal is achieved, and if not, add directions immediately:"
		}
	}
	input := lead + situational + "\n\nJudge the goals based on the situation above. When a goal is **truly achieved** (the goal result has been obtained / the target vulnerability confirmed), mark it with prove_goal one by one. **Hard floor: as long as the goal is not yet achieved and there are currently no open or running intents (frontier_open=0 and running_intents is empty), this round must produce at least one intent that advances toward the goal — there is no running work to wait for and no queued direction, so producing 0 intents = the task stalls. Only when open/running intents are already advancing, or the goal is achieved, may this round produce no new intents.**" +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration now interrupts the running tool when the wall clock is up and enters wrap-up
	// in place (on the live ctx), so a stuck round no longer bypasses wrap-up and no external
	// hard ctx backstop is needed. ctx only carries pause / kill / shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
