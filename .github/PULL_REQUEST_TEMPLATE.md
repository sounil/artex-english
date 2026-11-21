<!--
  This PR template is specific to artex-global (the ARTEX global edition).
  Please read CONTRIBUTING.md first for the contribution and localization policies.
-->

## Summary

<!-- In one or two sentences, describe what changes and why. -->

## Type of change

<!-- Put an x in the boxes that apply. -->

- [ ] Bug fix (`fix`)
- [ ] New feature (`feat`)
- [ ] Documentation (`docs`)
- [ ] Localization / translation (`i18n`)
- [ ] Refactoring / cleanup (`refactor` / `chore`)
- [ ] Other:

## Related issues

<!-- e.g. Closes #123 -->

## How it was verified

<!-- Note which commands you actually ran and what they confirmed. Attaching the output log helps. -->

- [ ] Go changes: `go build ./...` · `go vet ./agent/` · `go test ./agent/` pass
- [ ] web changes: `npm run check` · `npm run build` pass
- [ ] UI changes: screenshots attached

## Localization checklist

<!-- You may leave this section empty if the PR is unrelated to localization. -->

- [ ] New comments, logs, error strings, prompts, and tool descriptions are in English; translations live only in localization files (`web/messages/*.json`, the English-keyed tables such as `server/errmsg_catalog.go`, translated docs).
- [ ] Any CJK I added outside localization files is an allowed exception (legacy alias, functional matcher, language autonym, guard marker) per [CONTRIBUTING.md](../CONTRIBUTING.md#localization-policy).
- [ ] I did not hard-code UI strings; I added each key to every `web/messages/*.json` file, with `en.json` as the source.
- [ ] If I changed agent prompt wording (`agent/promptcatalog.go` · `agent_prompts`), I checked the agent's behavior in a local isolated environment (the English prompts are not yet re-benchmarked).
- [ ] I left commands, payloads, code, URLs, and raw tool output untranslated.

## Authorized-use confirmation

- [ ] This change does not encourage use beyond the [authorized scope](../README.md#️-read-first--authorized-use-and-legal-notice)
      or applicable law. I verified it only against targets I own or am authorized to test, or in a
      local isolated environment.
- [ ] I did not include test or scan output in the commit.
