# Provenance

This directory is the norma agent SDK, brought into the ARTEX-global source tree so the
build no longer depends on an external module.

- **Origin:** `github.com/Autumn-27/norma` v0.4.4 (`h1:wdifhqGJKiCvtGz5tv81C/dVA6eK5yvBWka3ZkoWeNU=`, the go.sum pin before the move)
- **License:** Apache License 2.0, see [`LICENSE`](LICENSE). It stays in force for this directory.
- **Import path now:** `github.com/Autumn-27/artex/norma/...`

## Changes from v0.4.4

- Import paths rewritten from `github.com/Autumn-27/norma/...` to `github.com/Autumn-27/artex/norma/...`.
- Left out: `go.mod`/`go.sum` (it is now part of the ARTEX module), `examples/` and `cmd/` (demos not
  used by ARTEX).
- `llm/retry_config_test.go`: `TestDoStreamHonorsConfiguredRetries` set no `HTTPClient` and panicked;
  it now uses the test server's client.
- Skipped tests that already failed in upstream v0.4.4 and document known limitations of the
  experimental noa context compaction (off by default in ARTEX): `noa.TestAdaptiveGrowthTracksTheWindow`,
  `noa.TestSuppressionReleaseFitsInsideTheWindow`, and in `noaadapter`
  `TestMostlyCompetentModelStaysBounded`, `TestLongSessionStaysBounded`,
  `TestLongSessionWithPartiallyCompliantModel`, `TestOverflowLearningIsWiredIn`,
  `TestTierTwoIsUnreachableOnSmallWindows`. Each carries a `t.Skip` with the reason.

Behavior is otherwise unchanged, including the WebFetch User-Agent `norma/0.4`
(`tool/webfetch.go`) that the detection rules in `detections/` key on.
