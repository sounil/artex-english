# Maintainer Guide

This document lays out how a **maintainer** keeps this codebase consistent: the English-only rule,
how to bring in outside changes, the checks to run, and the release pipeline. Contribution scope and
legal responsibility live in [CONTRIBUTING.md](CONTRIBUTING.md); user-facing guidance lives in
[README.md](README.md).

The code started as a fork of `Autumn-27/ARTEX` (currently
unreachable, checked 2026-10), by way of the multilingual `jiwoochris/artex-global` edition. Neither
repository is under this project's control, so treat anything taken from them as outside code: review
it before it lands, and never point builds, updates or images at them.

---

## 1. The structure at a glance

- **English only.** The UI, server messages, logs, notifications, reports, agent prompts, tool
  descriptions and documentation are English. There is no language setting and no translation file.
- **UI strings** live in one catalog, `web/messages/en.json`, looked up by key through `next-intl`.
  Do not hard-code screen text in components.
- **Agent output.** `langDirective()` in `agent/prompt.go` appends to each role's system prompt an
  instruction to write everything the user reads in English, even when the target, its evidence or a
  stored prompt is in another language.
- **Vendored code.** The agent SDK lives in-tree under [`norma/`](norma/) (see
  [`norma/NOTICE.md`](norma/NOTICE.md)); there is no external module dependency for it.

---

## 2. What stays in English, and the allowed CJK exceptions

Write everything in English. The agent prompts (`agent/promptcatalog.go`, the `agent_prompts` seed and
the tool descriptions) were originally benchmarked in Chinese and the English versions **have not been
re-benchmarked yet**, so treat any wording change in them as a behavior change and check it against a
local isolated sandbox. Existing installs keep the prompts already stored in their database (seeded
only when empty). Untouched built-in tool descriptions are migrated to English once at startup
(`server/tool_desc_legacy.go`, flag `tool_desc_english_v1`).

Commands, payloads, code, URLs, identifiers and raw tool output are never rewritten.

Only these deliberate Chinese, Japanese or Korean exceptions may remain in the source:

- **legacy aliases for old stored data**, so databases written by earlier versions still read and
  display in English: the `[模型]` model-verdict prefix (now `[model]`), Chinese chat-mention tokens such
  as `@[漏洞#12]` (now `@[finding#12]`), old default titles, old trigger words 意图/提示/의도/힌트 still
  accepted as input, legacy `[内置] ...` rule names (new seeds use `[Built-in] ...`), and the mapping in
  `web/src/lib/builtin-i18n.ts` that shows English labels for those seeds;
- **functional matchers** for outside input: provider quota error texts, ICP 备案 detection, a skill's
  grep pattern for Chinese login errors in target JavaScript, and CJK test inputs;
- **the guard marker** `【ARTEX 平台管控·非目标防御】` in `guard/guard.go`. The published detection rules in
  `detections/` match it byte for byte, so it is a detection indicator (see 4.2 and 5.4).

---

## 3. The upstream tracking base

Upstream is currently unreachable. The last upstream commit folded into this code is:

- **Base = `d003372`** (upstream `main`, 2026-10-03, merge of PR #189 `fix/sse-same-origin`).

Do not add `Autumn-27/ARTEX` (or `jiwoochris/artex-global`) as a git remote, and do not fetch, build or
pull images from either. If one of them comes back, nothing guarantees its contents match the history
this code was built from, and it may host altered or malicious code. If you want a specific change from
it, inspect it in a separate, disposable clone, confirm that `d003372` is an ancestor of what you are
looking at, and port the change by hand as outside code using section 4. Then record the new base using
section 7.

---

## 4. The procedure for bringing in outside changes

### 4.1 Inspect the difference

```bash
git fetch upstream
git log --oneline d003372..upstream/main           # list of unapplied commits
```

### 4.2 Classify the changed files

```bash
git log --name-status --oneline d003372..upstream/main
```

- **Agent prompts or tool descriptions** → port the meaning into the English text and check the
  behavior in a local isolated sandbox.
- **Go logic** (`db/`, `llmrec/`, `server/`, ...) → apply the logic, writing comments, logs and error
  strings in English.
- **UI** (`web/src/**`) → put new screen strings in `web/messages/en.json`, never hard-coded.
- **Upstream indicators pinned by the detection rules** (the prober User-Agent in `enrich/enrich.go`,
  the self-update User-Agent in `selfupdate/`, the audit marker in `guard/guard.go`, the
  destructive-command deny list in `db/db.go`, the default listen and recording-proxy ports in
  `cmd/artex/main.go`) → bring the Sigma/Suricata rules, the ATT&CK layer in `detections/` and
  `detections/indicators/artex_indicators.csv` in line with the new values. The indicator-match test
  in 5.4 catches a mismatch.
- **New external dependencies** (modules, images, download URLs, update sources) → do not add one this
  project does not control without a review; see the dependency notes in README.md.

### 4.3 Apply

Merge or cherry-pick feature by feature, rewrite any non-English text in English, then run the checks
in section 5.

---

## 5. English-only and drift checks

### 5.1 No CJK in the built UI

CI's web workflow runs this gate after the static export; run it locally the same way:

```bash
cd web && npm ci && npm run build:static && cd ..
python3 -I scripts/check-web-cjk.py   # fails on any Chinese, Korean or Japanese character in web/out
```

### 5.2 No new CJK outside the allowed exceptions

List added lines that contain CJK and review each hit against section 2 (replace `origin/main` with
the base you are comparing against):

```bash
git diff -U0 origin/main -- . \
  | grep -P '^\+(?!\+\+).*[\x{3040}-\x{30FF}\x{3400}-\x{9FFF}\x{AC00}-\x{D7AF}]'
```

### 5.3 Backend messages stay English

The `*_localized_test.go` tests assert that server, notification, report and intercept messages
contain no Han, Hangul or kana. They run with `go test ./...`.

### 5.4 Make sure the detection indicators still match the upstream source

The rules in `detections/` are based on the strings that upstream actually emits (prober User-Agent,
self-update User-Agent, audit marker, destructive-command deny list). When an upstream re-sync changes
these values, everything else still passes while only the shipped rules silently stop matching. The
test below verifies bidirectionally that each indicator is still present in both the upstream source
and the rules, so run it after a re-sync.

```bash
detections/tests/indicators/run.sh   # runs in isolation under Docker; RESULT: PASS means a match
```

On failure it prints which indicator is out of sync and in which direction (whether the upstream
source changed or the rule changed), so bring the rules and the layer in line with the new values per
the last classification criterion of 4.2. This test also runs automatically in the repository CI
([`.github/workflows/detections.yml`](.github/workflows/detections.yml)) on every push/PR that changes
the rule tree or the upstream source files above, catching re-sync drift at the merge gate.

If you add a new indicator and in doing so **pin a new upstream source file** (as when adding the port
indicator from `cmd/artex/main.go`, for example), you must also add that file to the `push` and
`pull_request` `paths` filters of the workflow above. If you miss it, a PR that changes only that
source will not trigger the indicator test, and the drift will silently pass the merge gate. The
indicator test checks this synchronization itself (its fifth check, "CI triggers this test when any
pinned source changes"): if any non-`detections/` source the test reads is not listed in both `paths`
blocks, the test fails, so source pinning and CI trigger conditions cannot be merged out of sync.

### 5.5 When bumping the detection-test tool pins

The detection tests run `sigma-cli`, the SigmaHQ validator plugin (`pySigma-validators-sigmahq`), and
the Suricata image at fixed versions (the defaults in each `run.sh`, overridable via environment
variables). Bumping these pins can introduce **tool-side drift** rather than upstream-source drift. In
particular, the SigmaHQ validator adds new convention checks with each release, so
`detections/tests/sigma_lint/run.sh` may surface new issues in red. When that happens, bring the rules
in line with the new convention, or — if the convention does not fit a standalone rule set — record
the reason and add it to the exclusion list in
[`detections/tests/sigma_lint/validators.yml`](detections/tests/sigma_lint/validators.yml). If a
backend plugin changes its support, the `sigma_backends` test gives the same signal.

---

## 6. Finishing with build and test verification

After applying a change, verify the backend and frontend per the
[development environment procedure in CONTRIBUTING.md](CONTRIBUTING.md#development-environment).
If you do not have Go locally, you can run the same thing under Docker.

```bash
docker run --rm -v "$PWD":/src -w /src \
  -v artexko-gomod:/go/pkg/mod -v artexko-gocache:/root/.cache/go-build \
  golang:1.26 sh -c 'go build ./... && go vet ./... && go test ./... -count=1'
```

When you add a user-facing backend string, add a regression test (`*_localized_test.go`) that asserts
it is English, so that if a later change pulls Chinese back in, the test catches it. Check agent output
language with a capable (frontier-class) model; small models can drift into another language.

---

## 7. Base-update record

Once you have applied an upstream change and finished verifying it, **update the base commit value in
section 3 of this document to the new upstream commit** and include that change in the same commit or
a following one. Doing so lets the next maintainer confirm "how far things have been applied" in this
one place.

Commit messages follow the [commit-message rules in CONTRIBUTING.md](CONTRIBUTING.md#commit-messages).
For example, an upstream-sync commit is written like this.

```
chore(upstream): apply upstream d003372..b55ceb1 (intercept token metering), English strings
```

---

## 8. Review and verification rules of thumb (common pitfalls)

Here are two pitfalls maintainers repeatedly fall into when checking upstream applies and documentation
improvements. Both are cases where "the checking method itself is wrong, so you
mistake something healthy for broken," so they are fixed here as rules of thumb to prevent unnecessary
reverts.

### 8.1 Check the repository's CI status by specifying the repository

If the local `git remote` has more than one GitHub repository registered (for example your `origin` plus
an `upstream`, see section 3), then if
you do not specify a repository in a `gh` command, `gh` **may pick the other repository as the
default** and show you run results from it, which does not have our workflows. You can then see
upstream CI green and **mistakenly think our CI passed**, or judge our workflows (`ci.yml`,
`detections.yml`) as "HTTP 404 ... not found" by mistake.

So when checking CI, always name the repository explicitly.

```bash
gh run list -R OWNER/REPO --workflow ci.yml --limit 5         # OWNER/REPO = your repository
gh run list -R OWNER/REPO --workflow detections.yml --limit 5
```

Once set, you can make `gh` default to our repository even when you omit `-R`. Note, however, that
this setting is a **local gh setting** and is not committed to the repository, so you must set it
again on a new machine or a new checkout.

```bash
gh repo set-default OWNER/REPO
gh repo set-default --view   # check that your repository shows up
```

### 8.2 Check external links in documents with GET, like a browser

Section 7 of the defense guide ([`docs/defense-en.md`](docs/defense-en.md)) carries links to Korean
official channels (boho.or.kr,
fsec.or.kr, pipc.go.kr). When checking whether these links are alive, using only `curl -I` (a HEAD
request) or the default User-Agent will **mistake a healthy link for a broken one**. Korean public and
security agency sites refuse a simple check for three reasons.

- **They reject HEAD requests.** For example, fsec.or.kr returns 400 to `curl -I` (HEAD).
- **They block the default `curl` User-Agent.** fsec.or.kr and pipc.go.kr return 400 even to GET
  requests sent with the default UA (they return 200 when sent with a browser UA).
- **They redirect to a different address.** pipc.go.kr redirects twice, from `www.pipc.go.kr` to
  `pipc.go.kr/np/`, so if you do not follow redirects you miss the final status.

So check links **with a browser User-Agent, with GET, following redirects**.

```bash
UA='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36'
for u in https://www.boho.or.kr https://www.fsec.or.kr https://www.pipc.go.kr; do
  curl -sS -L -A "$UA" -o /dev/null -w "$u -> %{http_code} %{url_effective}\n" "$u"
done
```

A final status code of 200 means the link is valid. If the status code comes back 400 or 403, first
suspect that the link is not broken but that **your checking method was blocked by the server's access
policy**, and re-check by eliminating factors one at a time: HEAD, the default UA, and not following
redirects. (As of the 2026-10-06 check, all three links return 200 with the method above, with
pipc.go.kr returning 200 after two redirects.)

This manual procedure is automated as-is by `scripts/check-external-links.py`. It gathers the external
links outside code fences and inline code from every tracked `.md` (excluding reserved and placeholder
hosts), checks their status with a browser UA, GET, and redirect-following as above, and retries on
network errors, 5xx, and 429 to separate transient flakes from real outages. It sorts results into four
classes: OK (2xx/3xx) · RESTRICTED (401/403/405/429 — the host is alive, only the checking method is
blocked) · ALLOWED (a known upstream-inherited dead link we cannot fix, listed in
`scripts/external-links-allowlist.txt`) · DOWN (404/410/5xx/connection error — likely broken).

- Preview the targets without the network: `python3 -I scripts/check-external-links.py --list`
- Release/periodic check (exits non-zero on a newly broken link): `python3 -I scripts/check-external-links.py --strict`

External-link liveness is flaky, so it is **not a merge gate**. Instead the non-blocking
[`external-links`](.github/workflows/external-links.yml) workflow runs `--strict` every Monday and on
manual dispatch, turning red when a DOWN not on the allowlist newly appears. Dead upstream links kept
only for attribution (for example the unreachable upstream repository) are ones we cannot fix, so they
go on the allowlist with a reason and are excluded from the strict check.

---

## 9. The release pipeline

Pushing a version tag (`v*`) makes [`.github/workflows/release.yml`](.github/workflows/release.yml)
build binaries for five platforms and, when a condition is met, a multi-architecture Docker image.
Earlier editions of this code published release tags (`v0.3.15-global.1`, `v0.3.15-ko.1`) from another
repository; none have been published from this one yet. This section records
what the pipeline assumes and produces, and whether those assumptions match the current repository
structure. Because pushing a tag creates a GitHub Release on the public repository, cut a release only
after the publishing decision is made.

### 9.1 How to cut a release

Pushing a tag that starts with `v` fires the workflow.

```bash
git tag v0.3.15
git push origin v0.3.15
```

### 9.2 What the pipeline does

The workflow is split into four jobs.

- **frontend.** Statically exports the frontend once (`web/out`) and uploads that output as the
  `web-dist` artifact. The binaries job below downloads and reuses this output per target.
- **binaries.** Cross-compiles five targets (linux amd64/arm64, darwin amd64/arm64, windows amd64) and
  packages a zip per target. On the linux amd64 binary it runs an `artex -h` smoke test to confirm the
  binary actually runs.
- **release.** Gathers all zips, generates a `SHA256SUMS` checksum file, and creates a GitHub Release
  with the zips and the checksum attached.
- **docker.** Takes the linux binaries cross-compiled by binaries, builds a multi-architecture
  (amd64/arm64) image, and pushes it to GitHub Container Registry as `ghcr.io/<owner>/<repo>`
  with the release tag and `latest`. It logs in with the workflow's built-in `GITHUB_TOKEN` (job
  permission `packages: write`), so no extra secrets are needed.

### 9.3 Do the build assumptions match the repository structure

The pipeline runs on the following three assumptions, and all three were confirmed to match the
current repository structure by reproducing the binaries job locally.

- **Frontend embed.** The binaries job receives the `web-dist` (the contents of `web/out`) uploaded by
  the frontend job into `server/webui/dist`, and `//go:embed all:webui/dist` in `server/webui_embed.go`
  embeds that location into the binary. The binaries job therefore does not rebuild the frontend; it
  calls [`build.sh`](build.sh) with `ARTEX_SKIP_FRONTEND=1`.
- **Binary and package paths.** `build.sh --target <os>/<arch>` produces the `dist/artex-<os>-<arch>/artex`
  binary and a zip package under `dist/`. The zip contains the binary together with a start script
  (`start.sh` on Linux/macOS, `start.bat` on Windows), `skills/`, `config.example.json`, and
  `README.md`.
- **Copying the binary into the Docker image.** The binaries job uploads the linux binary separately as
  the `bin-linux-<arch>` artifact, and the docker job receives it as `dist/<arch>/artex`. The
  `COPY dist/${TARGETARCH}/artex` in [`Dockerfile`](Dockerfile) picks up that path via the `TARGETARCH`
  that buildx fills in per platform during a multi-architecture build. [`.dockerignore`](.dockerignore)
  does not exclude `dist/`, so the binary is included in the build context.

### 9.4 The Docker image namespace

Images are published to `ghcr.io/<owner>/<repo>` of whichever repository runs the release workflow
(`IMAGE_NAMESPACE: ghcr.io/${{ github.repository }}`), so a release never pushes to a namespace this
project does not control. `docker-compose.yml` runs the locally built `artex-global:local` by default;
point `ARTEX_IMAGE` at your published image only if you want to. Upstream's `autumn27/artex` (Docker Hub)
and the earlier `ghcr.io/jiwoochris/artex-global` are outside this project's control and are not used. A
GHCR package may be created as private on its first publish; check its visibility in the package
settings after the first release.

### 9.5 Verifying locally without a tag

To check only the pipeline assumptions without cutting a public release, reproduce the binaries job
locally. If you do not have Go locally, you can run the same thing under Docker.

```bash
# 1) Static frontend export (corresponds to the frontend job in release.yml)
cd web && npm ci && npm run build:static && cd ..
# 2) Place it where the binaries job receives the artifact
rm -rf server/webui/dist && mkdir -p server/webui/dist && cp -a web/out/. server/webui/dist/
# 3) Build one target with the same environment as the binaries job
docker run --rm -v "$PWD":/app -w /app \
  -e ARTEX_SKIP_FRONTEND=1 -e ARTEX_SKIP_NPM_CI=1 \
  -e ARTEX_COMPRESS=0 -e ARTEX_PACKAGE=1 -e ARTEX_PACKAGE_DIR=dist \
  -e ARTEX_BUILD_VERSION=v0.0.0-local \
  golang:1.26 bash -c 'apt-get update && apt-get install -y zip && ./build.sh --target linux/amd64'
# 4) Confirm the outputs: dist/artex-linux-amd64/artex · dist/*.zip · dist/SHA256SUMS
```

`dist/artex-linux-amd64/artex` is a statically linked ELF, and given `-h` it prints usage and exits
with code 0. This is the behavior the binaries job's smoke test confirms. Build outputs
(`dist/`, `server/webui/dist/`) are not committed to the repository (they are excluded by
`.gitignore`).
