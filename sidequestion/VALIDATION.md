# `/btw` Validation Record

Date: 2026-09-10. Branch: `codex/btw-side-question`. Baseline commit: `8dae851b9b622f2ff2631f332fde9719d0b16fba`.

An isolated PostgreSQL test DB and data directory were used. Real model credentials were injected only into the isolated test environment and never written to code or this record, and the product's default model was not changed. Go 1.26.3, norma v0.3.6, Next.js 16.2.9.

The real model conversations, returned objects, engineering assertions and the raw Qwen review text are stored in [validation-2026-09-10.json](validation-2026-09-10.json), which contains no API credentials.

## Engineering checks

All items below pass; the supporting test is given in parentheses.

- Deep copy of structured messages and tool arguments: pass (`TestCheckpointDeepCopyAndBoundaries`).
- Summary/compaction requests do not overwrite; completed responses and terminal states are published; half-finished responses are excluded: pass (`TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember`).
- Identity of the actual model-pool member: pass (`TestSnapshotExcludesPartialStreamAndSelectsPoolMember`).
- Tool pairing, 20-exchange replay, budget trimming and over-budget errors: pass (`TestBuildRequestCompactionToolPairingAndBudget`).
- Main and side question running in parallel, cancellation isolated in both directions: pass (blocking Provider, `TestMainSideConcurrencyAndIndependentCancellation`).
- No tool execution, streaming and non-streaming, usage already accrued at the point of failure: pass (`TestServiceNoToolsAndUsageOnFailure`).
- Real norma ChatAgent with the local Read tool, isolation from the main transcript and activity: pass (streaming and non-streaming subcases of `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`).
- Persistence, pagination, idempotency, partial answers kept after restart: pass (`TestSideHistoryIdempotencyPagingAndRecovery`).
- Clear racing late writes, parent resource deletion, version comparison: pass (`TestSideClearLateWritersAndDeletedParent`).
- MainAgent/Worker archive and restore (v1, v2, v3): pass (`TestSideTaskArchiveVersions`).
- All three parent interfaces, authentication, resource ownership, Worker logical deletion: pass (`TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart`).
- Side questions work while the main session is busy; independent SSE reconnect/disconnect, cancel, clear: pass (`TestSideHTTPBusyIsolationClearAndReconnect`).
- One concurrent run per parent session, four globally: pass (two `TestSideHTTP…` cases).
- Snapshot saved before submission, follow-up after restart, old sessions cannot forge a snapshot: pass (`TestSideCheckpointPersistsBeforeAdmissionAndRestart`).
- Refuses to continue when a cached config is deleted or its model changes: pass (`TestSideRejectsDeletedOrChangedCachedProfile`).
- Cancels before archiving and waits for the final answer and usage to be saved: pass (`TestSideTaskDrainPersistsBeforeArchive`).
- Usage recorded exactly once and attributed to the side question when a streaming consumer cancels early: pass (`TestSideUsageRecordedOnceOnConsumerCancellation`).
- Worker/deadline runtime contexts auto-restored after a restart keep publishing new snapshots: pass (`TestSideRestoredWorkerRuntimePublishesNewCheckpoint`).
- Race checks of the affected packages: pass (commands below).
- TypeScript and production build: pass (`npx tsc --noEmit`, `npm run build`).
- Biome check of the newly added frontend modules: pass (`biome check`, 3 new modules).

The automated checks can be reproduced by setting `ARTEX_PG_DSN` to a throwaway database (do not point it at a production DB):

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

The full Go regression suite is not entirely green. Two existing tests in the `server` package fail during temporary-directory cleanup, both reporting `TempDir RemoveAll … directory not empty`:

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

Checking out the unmodified baseline commit above and re-running the `server` package in the same isolated environment reproduces both cleanup failures identically. The baseline run also showed a separate target-node-count assertion failure in `TestCoreTaskLifecyclePG`, which does not occur in the `server` regression of the final revision. Other packages pass, as do the side-question cases and race checks added here. Baseline problems were not marked as passing in this review, and no existing assertions were changed to hide problems.

The Next.js build prints existing warnings about inferring the root from multiple lockfiles/workspaces, but the build completes and all pages are generated successfully.

## Browser checks

The Codex built-in browser was connected to an isolated local Go service and Next.js dev server. The following manual and automated interactions were performed on desktop and on a narrow 390 × 844 screen, and screenshots and browser logs were checked:

- Typing `/btw` while a regular chat is running shows the main content and the side question at the same time, and the desktop sidebar works correctly.
- Asked a follow-up question. Stopping the side question keeps the part already generated, and the main flow continues.
- Closing the panel keeps the request running, and reopening restores the completed answer. After reloading the page, typing `/btw` alone restores the history.
- In the narrow-screen Drawer, input, buttons, history and closing all work, with no horizontal overflow.
- Clear shows a confirmation dialog; after clearing, the history is gone but the main transcript and snapshot remain.
- Asking the task's MainAgent and two Workers separately and switching between them never mixed up agent tabs or histories.
- With a Worker kept running by a blocking local model fixture, `/btw` was submitted from the Worker's main input box. After the side question was stopped, the Worker still showed its live running state and its own pause button, and the side question saved its partial answer.
- The browser error/warning log is empty.

The controllable fixture was used to verify concurrency timing precisely and does not depend on a real model's output speed. Two Worker run checks during debugging did not produce a valid concurrent window (the task had already finished or the answer ended early); the fixture was fixed and the checks were re-run and passed. Those early attempts are not counted as valid passes.

## Real model conversations

`grok-4.6` was probed first. The OpenAI-compatible endpoint is `http://127.0.0.1:12580/tingly/openai`. The probe returned HTTP 200 with the model name `grok-4.6` and `READY`, taking 2.82 s. Since the first choice was available, the fallback chain of Tingly's `glm` or Zhipu's `glm-5.3` was not enabled, and those two fallback services were not validated this time.

- Asked about the asset, goal and marker while the main session was running: returned `redhaze.top`, reading the home page and a goal summary, and `BTW-REAL-0910`; the side question completed (16.97 s).
- Asked about tool evidence after the main session finished reading the home page: accurately cited WebFetch 200, curl's 301 → 302 → 200 redirect, and the page title (7.24 s).
- Side question asked to create a test file with Bash: refused to execute, and the target file was not created (7.74 s).
- A completed side question does not change the main context: the main transcript's SHA-256 and the main activity record matched, and the side question's tool execution count was 0.
- Follow-up after actually stopping and restarting the Go service: the previous 3 side-question records were kept, and the asset, marker and title were answered directly from the persisted snapshot without re-running the main agent.
- Grok non-streaming config used in a new session: correctly answered the asset and `ATOMIC-0910`, and returned and saved usage (input 11734, output 138, cache_read 11520).

In the asset case the main session read the public home page with WebFetch and Bash/curl; the landing page was `https://id.redhaze.top/home`, and the title returned by the page was a Chinese title reading "RedHaze Technology RedHaze Group · Global Group Portal" (returned verbatim by the model). Bash only saved the response to a local test file temporarily and performed no remote writes. This was confirmed separately from the fact that "the side question did not execute tools".

Main transcript checksum: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**Usage limitation:** Tingly's Grok streaming responses returned no usage. A separate check sending `stream_options.include_usage=true` directly got HTTP 200, 12 data frames and 0 usage frames. So the 0 in the streaming tests means the endpoint does not provide usage and must not be read as "no charge". Non-streaming usage and the fixture's failure/cancellation usage were all saved correctly.

## Qwen review

The review model was `qwen-flash` via the OpenAI-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1`, HTTP 200. It was given the first three real side-question conversations, the main session's tool evidence and the engineering assertions, and returned `verdict: accept` and `concerns: []`. It judged that the answers matched the asset, marker and page-read evidence and that the side question's tool refusal met the constraints. Review usage: prompt 6625, completion 312, total 6937.

This Qwen review does not cover the service restart and non-streaming tests added later. Qwen's generalization of "no writes" is too broad: the main session's curl did create a local temporary response file, as recorded clearly above. Concurrency, zero tool executions and transcript isolation are judged by engineering assertions; the model review only assists in assessing answer quality.
