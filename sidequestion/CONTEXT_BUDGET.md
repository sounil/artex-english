# Long Side-Question Conversations and the Context Budget

Revised 2026-09-11. The previous implementation treated the character count of the request JSON as its token count and inherited the main task's 32K output reservation, so it rejected legitimate side questions up front when there was a lot of HTML, JS or tool output.

## Comparison with open-source implementations

- [Grok CLI side-question context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): excerpts fragments from the most recent user and assistant text, with a character budget of about 2000 and at most 400 characters per item. This path keeps no history of consecutive side-question exchanges.
- [Grok CLI independent request](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): uses its own cancellation signal. The output cap is 2048 tokens when the model supports it. No tools are provided.
- [Grok CLI main-session compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): estimates tokens, keeps recent content, folds new content into the previous summary, and handles cuts that cross turn boundaries.
- [OpenCode session compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): estimates the whole request, reserves output and buffer, adds a rolling summary to recent content, and sends tool-less summary requests. By default it uses a recent budget of 8000 and a summary output cap of 4096 tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): attempts overflow recovery only when assistant output has not started yet, and calls made after recovery do not re-enter the same overflow-recovery path.

ARTEX borrows the ideas of an independent output budget, recent content plus a rolling summary, and bounded recovery. It keeps norma v0.3.6's structured messages and tool pairing intact rather than adopting Grok's text-excerpt approach, and it does not record OpenCode-style main-session compaction events in the ARTEX main transcript.

## Request budget and execution

- Messages are estimated the same way norma does it — UTF-8 bytes per content block, plus a 4/3 safety factor. The system prompt, tool schemas and message-wrapping overhead are counted on top. This estimate is not the model's exact token count.
- Side-question output defaults to at most 8192 tokens and never exceeds an output cap already set in the main config. The service environment variable `ARTEX_BTW_MAX_OUTPUT_TOKENS` sets a cap in the range 256–32768; it does not change the product's default model or the main task's parameters.
- The input budget is the context window minus the output cap and a safety margin; when the window size is unknown the platform default of 200K is used. The safety margin is 5% of the window, at least 128 and at most 8192 tokens.
- Successful exchanges are loaded by increasing ordinal, at most 20 at a time. Up to 20 exchanges are kept verbatim, within a token budget of 1/4 of the input budget and no more than 16K.
- Exchanges that exceed the budget are folded into a rolling summary. The summary carries the source records and context time. Earlier assistant answers are not equivalent to new tool evidence, so on conflict the latest main snapshot wins.
- If the main context is still too long, only the older messages of the copy are summarized and up to 8K tokens of recent content are kept. The cut point never splits a tool call from its result. If a single group is too large, the whole group goes into the summary.
- Summary input is split UTF-8-safely to fit the window actually remaining, with an output cap of 2048 tokens. Empty summaries, truncated responses, tool calls and summaries over budget are never written to the cache. A single side question makes at most 12 summary calls, all under the same 120-second timeout. On hitting the cap it fails clearly instead of looping forever.
- If the model first reports a context overflow and no text or tool call has been emitted yet, the request is reduced further and retried at most once. If the estimated size does not shrink, recovery stops immediately. Other model errors or partially streamed output do not trigger this recovery.
- All usage already counted — including summaries, failed attempts and usage at cancellation — accumulates on the same side-question request. When the Provider returns no usage it is recorded as 0; estimates are never presented as real usage.

## Persistence and UI

`side_question_sessions.memory` stores the summary of older exchanges, the folded-through ordinal, and the main-context summaries cached per snapshot identity. `side_question_requests.context_info` stores the preparation phase, the number of exchanges actually replayed, the summary usage breakdown and the budget estimates.

A summary is saved only while the originating request is still running and the clear version matches. Clear wipes the cache as well, and late-arriving writes cannot resurrect cleared data. A new snapshot does not reuse the previous snapshot's summary. The summary fields are saved with v3 task archives; restoring an older v3 that lacks the fields fills them with an empty object, and v1 and v2 remain compatible.

POST accepts and returns the request first; preparation and compaction run in the background without holding the admission lock or a database transaction. The SSE stream and history view show the preparation, exchange-summarizing, copy-compaction and answering phases. A compaction failure is saved as the failed terminal state of that side-question request. The frontend keeps the error, restores the draft of the failed question, and does not cover the input box with a floating notice. History polling no longer clears submission errors.

## Validation record

- Replay of 19, 20, 21 and 50 exchanges, preservation of older conclusions beyond 20 exchanges, and reuse of the summary cache after restart: automated tests pass.
- Very long Chinese answers with code context, request-budget chunking, tool pairing, snapshot immutability and cache invalidation on a new snapshot: automated tests pass.
- Summary failure, cancellation, truncation, over-length and tool returns, the clear race, the call cap, exactly-once overflow recovery and no retry of partial streams: automated tests pass.
- On an isolated PostgreSQL: pagination, restart, v1/v2/v3 archives, archive restore of the summary cache and budget metadata, an older v3 without the new fields, and 20 parent sessions sharing four concurrent slots: pass.
- Go candidate service build, frontend TypeScript check, Biome check of the modified components, and a Next.js production build in a separate directory: pass.
- Using the built-in browser with a standalone UI fixture at 1280×720 and 390×844, verified the exchange-summarizing phase, the summary-scope notice, draft restore after failure, the absence of floating error notices, no horizontal overflow and no console errors. The temporary fixture was removed.
- A read-only replay of an existing local Worker snapshot passes the new budget check. For example, Worker #3's 293,085-character snapshot is no longer wrongly rejected because of the local character count. No external model call was involved.
- The real conversation test that would have sent a private Worker snapshot to Grok was rejected by the auto-approval review and never ran, so it does not count as a pass.
- At 2026-09-11 00:37 the local backend was restarted at the user's request with the original database, data directory and login settings. The SHA-256 of the running executable matched the candidate binary, and `/api/health` returned healthy through both the backend and the frontend proxy.

Validation commands (isolated test DB only):

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artex
npx tsc --noEmit
npm run build -- --webpack
```

The frontend production build used a separate copy so it would not overwrite the current preview's `.next`. The candidate service lives in `/private/tmp/artex-btw-budget-candidate` and was copied to `/private/tmp/artex-btw-preview/artex` to run. The original binary was backed up as `artex.before-context-budget` in the same directory.
