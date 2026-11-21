<!--
  This PR template is specific to artex-global (the ARTEX global edition).
  Please read CONTRIBUTING.md first for the contribution and English-only policies.
-->

## Summary

<!-- In one or two sentences, describe what changes and why. -->

## Type of change

<!-- Put an x in the boxes that apply. -->

- [ ] Bug fix (`fix`)
- [ ] New feature (`feat`)
- [ ] Documentation (`docs`)
- [ ] Refactoring / cleanup (`refactor` / `chore`)
- [ ] Other:

## Related issues

<!-- e.g. Closes #123 -->

## How it was verified

<!-- Note which commands you actually ran and what they confirmed. Attaching the output log helps. -->

- [ ] Go changes: `go build ./...` · `go vet ./agent/` · `go test ./agent/` pass
- [ ] web changes: `npm run check` · `npm run build` pass
- [ ] UI changes: screenshots attached

## English-only checklist

- [ ] New UI text, comments, logs, error strings, prompts, tool descriptions and docs are in English. UI strings go in `web/messages/en.json`, not hard-coded.
- [ ] Any Chinese, Japanese or Korean I added is an allowed exception (legacy alias for old stored data, functional matcher, guard marker) per [CONTRIBUTING.md](../CONTRIBUTING.md#english-only-policy).
- [ ] If I changed agent prompt wording (`agent/promptcatalog.go` · `agent_prompts`), I checked the agent's behavior in a local isolated environment.
- [ ] I left commands, payloads, code, URLs, and raw tool output unchanged.

## Authorized-use confirmation

- [ ] This change does not encourage use beyond the [authorized scope](../README.md#️-read-first--authorized-use-and-legal-notice)
      or applicable law. I verified it only against targets I own or am authorized to test, or in a
      local isolated environment.
- [ ] I did not include test or scan output in the commit.
