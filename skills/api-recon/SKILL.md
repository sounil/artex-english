---
name: api-recon
description: Use this skill when collecting a website's API endpoints.
---

# API Recon (front-end endpoint reconnaissance)

Within an **authorized** scope, discover as completely as possible: **backend APIs** (paths, methods, parameters, response bodies), **front-end routes**, and **UI feature trigger points** (tabs, dialogs, table actions, etc.).

---

## Boundaries and prohibitions (mandatory for agents; violating them is out of scope)

This skill does **API / parameter-surface reconnaissance only**. It is not a vulnerability-hunting or exploitation phase.

### Task boundaries

| Area | Allowed | Prohibited |
|---|---|---|
| **Goal** | Enumerate paths, methods, parameters, routes, UI trigger points | SQLi/XSS/privilege escalation/brute force/vulnerability fuzzing, tampered-request attacks, destructive operations |
| **Auth** | Hook + stub/mock to bypass the **client-side** login gate | Asking the user for, or guessing, usernames/passwords; submitting a real login form |
| **Runtime** | Hook endpoints without credentials and use mock responses to get the SPA into its post-login shell | Flows that can only continue with a real backend session |

### Credential-free dynamic analysis (Phase 3 default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap endpoints such as login, permissions and menus;
2. For business query endpoints, return mock bodies that are **structurally correct, carry a success business code, and may have empty data**;
3. This lets the front end render post-login pages even with no backend or a 401 environment, triggering more XHR/fetch/WebSocket traffic;
4. **Empty data, blank tables and placeholder UI are expected**; do not switch to real login or vulnerability testing because of them.

**In one sentence**: use mocks to open up front-end routes and component mounting, and **record only outbound requests**. What the backend returns does not matter; what matters is **which other endpoints the front end will call**.

### Hard process prohibitions

| Prohibited | Do this instead |
|---|---|
| grep/curl/Read the main entry `index-*.js` to extract API paths before Phase 1 is complete | Run `OUTDIR/harvest_static.py` |
| Hand-writing `extract_apis.py` or similar scripts to replace harvest | Edit `OUTDIR/harvest_static.py` and rerun |
| Repeating the same grep/command after it has failed ≥2 times | Change strategy: read tool_logs, edit harvest, check the reference |
| Skipping gates A/B and running the original `scripts/` directly | Copy into OUTDIR and adapt to the target |
| Real username/password, OTP, OAuth or other authentication | stub/mock (see above) |
| Skipping stubs "to get real data" and doing privilege-escalation/injection tests | Record outbound only; that is the recon boundary |
| Deleting, exporting sensitive data, bulk writes or other irreversible operations | Same applies to coverage clicks |
| Claiming all pages and endpoints were found without completing runtime + dynamic enumeration | See "Definition of done", or state the limitation |
| Claiming all parameters are known without completing the parameter trigger matrix + diff | Phase 3b matrix + Phase 5 diff |
| Inferring required/optional from a single runtime sample | Multi-sample diff, or validation rules / error-based inference |

---

## Two-layer model + run modes

| Layer | Output | Limits |
|---|---|---|
| **Static** (JS bundle) | Full endpoint paths, draft routes, candidate fields at payload-building sites | No HTTP methods; parameters need Phase 1b; misses URLs assembled at runtime |
| **Runtime** (live session) | Method + body + response + dynamic URLs + WS/SSE; multi-sample diff fills in parameters | A page must actually render to send requests; one sample is not enough to decide required/optional |

| Run mode | Engine | Use for |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response bodies, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Clicking tabs/dialogs/tables for deeper feature coverage |
| **both** | depth first, then coverage | Most complete, takes longest |

**Parameter methodology** (no general-purpose script): find paths with harvest/regexes; find parameters with **anchor window expansion + UI binding chain + multi-sample diff + error-based inference** (grep recipes in section J of [reference.md](reference.md)).

---

## Definition of done

All of the following must hold before you may claim recon is complete:

- [ ] **Static**: Phase 1 harvest produced `api_static.txt`, `routes.txt`, `js/`
- [ ] **Runtime**: at least one of depth or coverage; coverage/both require **working hooks + the dynamic enumeration loop**
- [ ] **In the shell**: visiting a business path does not land on `/login` (watch out for hash routing)
- [ ] **Parameters**: coverage/both completed the parameter trigger matrix + `param_samples.json`; Phase 5 merged `params_merged.json`
- [ ] **Depth** (if module pages are blank): Phase 4 permission tree reconstructed and rerun until **module-level APIs** appear (not just locale/bootstrap)
- [ ] **Delivery**: all Phase 5 outputs present (see the Phase 5 output table); `insert_assets` wrote the service and endpoint assets

---

## Scripts and gates

`scripts/` contains reference templates only. **Do not** run the originals directly and treat the output as final.

**Rule**: read first → adapt to the target → write into `OUTDIR` (e.g. `recon/`) → record in `CHANGES.md`. If it doesn't fit, rewrite following the methodology and borrow only the structure.

| Gate | When | Reference script → OUTDIR copy | Usual required changes |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **The default regexes work as-is on most sites**; only when the manifest/dialect doesn't match, change the endpoint regex, webpack/Vite `publicPath`, MPA exclude/cookie |
| **B (runtime)** | After Phase 2, before running depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success value, stubs, login regex, API prefix, hash/history |

**Mandatory SPA order** (not interchangeable; Phase numbering takes priority over "explore first, script later"):

| Step | Must | Prohibited |
|---|---|---|
| After Phase 0 | The next Bash call = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500KB) |
| Gate A | Copy the script → small changes as needed → **run it immediately** | Extracting APIs by hand first and then deciding whether to harvest |
| Before Phase 1 is complete | Check the outputs with `wc -l`; on 404s, fix harvest and retry | Hand-written extract scripts; repeatedly grepping URLs that were never downloaded |
| From Phase 1b on | grep only `OUTDIR/js/*.js` | Using the main bundle instead of harvest |

- ✅ Copy `harvest_static.py` → (optionally) adjust the regex → **run immediately**
- ❌ curl the main bundle → grep several times → write a temporary extractor → harvest only at the end
- **MPA**: after Phase 0, the next Bash call = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and output constraints

| Constraint | Details |
|---|---|
| Large files | **Do not** Read/grep `index-*.js` files >100KB into context; batch-process them with the OUTDIR scripts |
| grep output | Must use `\| head -20` or `-m 5`; keep only path summaries in the conversation, do not paste bundle fragments |
| Verification | Use `wc -l`, `ls \| wc -l`; do not Read whole directories |
| Initial regex probing | Optional, at most once, only on small chunks ≤50KB or HTML; harvest is authoritative for the real static pass |
| reference | Recipes/templates/troubleshooting are in [reference.md](reference.md); do not repeat them inline in full |

---

## Execution roadmap

```
Phase 0 classify + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b parameter reverse engineering
  → Phase 2 the three auth gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree (if needed) → rerun Phase 3
  → Phase 5 merged report + insert_assets to bulk-insert every discovered service and endpoint API asset; under no circumstances may already-discovered assets be left out of the insert
```

Check items off in order; **do not enter the next Phase until the previous item is complete**.

1. [ ] **Phase 0**: initial SPA/MPA probe; create `OUTDIR` → [Phase 0](#phase-0--classification)
2. [ ] **Gate A + Phase 1**: copy scripts → harvest **immediately** → verify with `wc -l` → [Phase 1](#phase-1--static)
3. [ ] **Phase 1b**: anchor window expansion + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b--parameter-reverse-engineering)
4. [ ] **Phase 2**: the three auth gates → `config.json` → [Phase 2](#phase-2--the-three-auth-gates)
5. [ ] **Gate B**: adapt the runtime scripts → [Phase 3](#phase-3--runtime)
6. [ ] **Phase 3**: depth / coverage / both; confirm you are in the shell; parameter trigger matrix → `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4--permission-tree-reconstruction)
8. [ ] **Phase 5**: merged outputs + report + `insert_assets` → [Phase 5](#phase-5--merge-and-report)

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (do not modify the skill's own `scripts/`):

- **SPA**: empty shell + `<div id=app>` + chunks → Phases 1–5
- **MPA**: SSR + `<form>`, no endpoint bundle → after gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Produces `forms.txt`, `links.txt`, `api_inline.txt`. If it is an SPA and forms ≈ 0 → switch to Phase 1.

---

## Phase 1 — Static

Follow [Scripts and gates](#scripts-and-gates) · [Tool and output constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse HTML scripts → webpack/Vite manifest → download every lazy chunk → produce `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Chunk count vs. manifest: on 404s, fix harvest and retry; do not curl chunks one by one by hand
- Too few entries in `api_static.txt` → loosen the endpoint regex in OUTDIR and rerun (see the reference)

### Phase 1b — Parameter reverse engineering

Paths come from Phase 1; parameter fields need their own recon. grep rules are in [Tool and output constraints](#tool-and-output-constraints).

**Done when**: for the important endpoints you can answer field name, transport location, inferred type, required or not, sample value, confidence.

#### 1b.0 — Transport shape

| Shape | Where the parameters are | Look first in static code at |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` next to the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Classic form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route table + `useParams` / `$route.params` |
| Encrypted/signed | Wrapped into `sign`/`data` | Hook the encryption function's inputs (reference section D) |

Output: tag each endpoint with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Anchor window expansion

Use known paths as anchors and widen the window to find the payload-building object:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Shared request helper | Global fields injected by interceptors |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Hook's second argument |
| Vue composable | Composable arguments |

Type remnants: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Technique |
|---|---|
| Form submit | Follow submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Route | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

Use the DevTools call stack to trace upward from `fetch`/`XHR.send` to the payload-building function.

#### 1b.3 — Three payload questions (≠ the Phase 2 three auth gates)

| Question | What to answer |
|---|---|
| **Assembly** | Where the payload is built, traces of transforms |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

While working the interceptor gate (Phase 2), also read the globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Hand-off to Phase 3

Candidate fields come from static code/the binding layer; **required/optional/conditional dependencies** need the Phase 3 parameter matrix + diff + Phase 5 error-based inference.

---

## Phase 2 — The three auth gates

grep in `OUTDIR/js/` (with `head`) and write the results into `config.json` (recipes in the reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How does the app decide it is logged in? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers the jump to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat a localStorage key name as a credential; confirm it from the chunks/request chain.

**Exit = gate B**: put the conclusions into `config.json` and edit `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API observation (optional)

Use the `preload.js` in OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Setting | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Sub-APIs found inside responses |
| `observe.storageReads/cookieReads: true` | Feed back into config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export after every coverage round: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3 — Runtime

Gate B must already be passed; follow [Boundaries and prohibitions](#boundaries-and-prohibitions-mandatory-for-agents-violating-them-is-out-of-scope) · the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in the reference).

### Hooks and stubs (shared by depth + coverage)

| Tier | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stubs | Get past first-screen auth |
| L2 negative correction | All JSON responses | Not-logged-in code → success |
| L3 fallback | `/api` etc. not matched by L1 | Empty success body to open up the UI |

- **depth**: fake auth + `forward` to rewrite business codes + `stubs`; walk `routes` (hash/history); produces `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or a userscript)

Verify: `window.__API_RECON_PRELOAD__` exists; business paths do not bounce back to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b — Coverage dynamic enumeration (mandatory)

1. Main navigation/sidebar — click every item, wait 1–3s for network
2. Tabs — `role=tab`, `.ant-tabs-tab`
3. Tables — view/edit/details on the first row
4. Toolbar — export, filter, create (**avoid irreversible deletes**)
5. On entering each module — merge APIs/routes
6. SPA — controlled `pushState` to paths in `routes.txt` not yet covered (prohibited for MPAs)

**Parameter trigger matrix** (mandatory): for each module, record once per operation type and **diff the samples**:

| Operation | Parameters it usually adds |
|---|---|
| List first screen | Pagination + default filters |
| Click search | keyword, filter |
| Advanced filter | More optional fields |
| Create/edit | Full entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Outbound bodies/headers are still real under stubs**; treat the request as authoritative. Record → `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4 — Permission tree reconstruction

**Trigger**: module pages are blank / every route shows only bootstrap calls (e.g. locale) → the content gate has not been passed.

| Symptom | Meaning |
|---|---|
| Got into the shell | Render gate + interceptor gate passed |
| Sidebar items missing / clicks show blank | Stub shape or permission codes incomplete |
| Every route has the same, very few APIs | `v-if permission` not satisfied |
| `routes.txt` far smaller than the bundle | Must be completed from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Stub checks: the outer `response_code` matches the interceptor gate; flat codes line up with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. For large SPAs you can tune `waitUntil`, `routeTimeout`, `perRouteMs` (see reference sections A3/I).

---

## Phase 5 — Merge and report

### Output table

| File | Phase | Content |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Static candidate parameter fields |
| `config.json` | 2 | Three gates + runtime config |
| `runtime_api.json` | 3a | Detailed depth recording (including WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click log, detail |
| `route_map.json` etc. | 4 | Permission tree intermediate files (if run) |
| `params_merged.json` | 5 | Merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, params, features, limitations |
| **insert_assets** | 5 | Write all service and endpoint assets to the asset store |

### 5b — Parameter merge

Diff from `param_samples.json`; **there is no general-purpose merge script**. Confidence rules are in reference J7 (high/medium/low/untriggered).

### 5c — Error-based inference

Within the authorized scope you may send incomplete requests and read the 400s (**this is parameter recon, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Watch for a `data` wrapper, `variables`, and pre-encryption `bizData`.

The report must state: runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a `CHANGES.md` summary of changes relative to the reference scripts.

Suggested `site_map.json` structure:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

More fields and grep recipes are in [reference.md](reference.md).

---

## General notes

- **Framework-agnostic**: the method is the same for webpack/Vite/Angular lazy loading
- **Transports**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope
- **SSR**: client-side fetches can be recorded; RSC/Server Actions cannot be fully enumerated
- **Blind spots**: JSVMP, WASM, strict HMAC/mTLS checks → static + state the limitation
- **Parameter blind spots**: conditional linkage, hidden params, WASM payload building → "untriggered" / "unreachable"
- **Static is the safety net**: when runtime is blocked, static analysis can still enumerate endpoints

---

## Additional resources

- Grep recipes, `config.json` template, troubleshooting, hooks, section J on parameter reverse engineering, site_map template: **[reference.md](reference.md)**
- Reference script paths are in the [Scripts and gates](#scripts-and-gates) table
