# Changelog

Notable changes to Skeptic. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `version` now reports the module version when a binary is built by
  `go install` rather than by goreleaser. The documented install command
  previously produced a binary that called itself `dev`.

### Changed

- **License is Apache-2.0**, restoring the project's intended license. v0.1.0
  shipped under MIT in error; see the note under that release below.
- Module path is `github.com/bugyal/skeptic`. The previous path did not exist
  on GitHub, so `go install` could not have worked.
- README opens with one pitch line instead of two, and carries CI, release and
  pkg.go.dev badges.
- `results/` has an index stating how to read the findings.

## [0.1.0] - 2026-09-24

> **Licensing note.** The v0.1.0 archives carry an MIT `LICENSE` in error. The
> project is Apache-2.0; v0.1.1 corrects the shipped artifacts. The v0.1.0 tag
> is immutable in the Go module proxy and is left in place rather than rewritten.

First release. Skeptic runs control experiments over a coding-agent benchmark
and reports which tasks are not measuring what they claim to.

### Added

- **`skeptic check`** — runs two controls against every task in a benchmark.
  `oracle` applies the reference solution and must score 1.0; `nop` changes
  nothing and must score 0.0. Tasks are classified `CLEAN`, `NOP_PASSES`,
  `ORACLE_FAILS`, `BOTH`, `NO_ORACLE`, `UNSUPPORTED` or `ERROR`. Non-zero exit
  on anything flagged, so it drops into CI unchanged.
- **The `partial` control** (`--partial`) — applies the reference patch with one
  hunk withheld. A score that stays at 1.0 means the suite never graded that
  hunk. This targets the weak-test failure mode, which neither `oracle` nor
  `nop` can detect, and which accounted for about a third of apparently-passing
  SWE-bench patches. Advisory: it warns and never fails a run.
- **`skeptic lint`** — static structural checks with no Docker, no model and no
  cost. Includes answer-leak detection: whether the fix is reachable from the
  instruction text (links to the PR that fixed it, an embedded diff) or from the
  agent's own filesystem (`solution/` or `tests/` copied into the image).
- **`skeptic report`** — re-renders any past run as a table, as JSON, or as
  markdown ready to paste into an upstream issue.
- **`skeptic version`** — version, commit, Go toolchain and detected Docker API.
- **Adapters** for Harbor / Terminal-Bench 2.x (`task.toml`), Terminal-Bench 1.x
  (`task.yaml`), and SWE-bench dataset exports (JSONL/JSON).
- **Evidence capture** — per task and per control, the full stdout, stderr,
  combined stream, exit code and raw score artifact under
  `.skeptic/runs/<timestamp>/`. This is what gets opened when a flag is disputed.
- **Versioned report schema** (`docs/report-schema.json`, schema 1), validated
  against generated reports in tests.
- Prebuilt binaries for linux and darwin on amd64 and arm64.

### Design notes

Three decisions exist to keep the tool from reporting a result it did not earn,
since a false verdict on a benchmark is worse than no verdict:

- A reward artifact that does not resolve to a single unambiguous score is an
  `ERROR`, never an average or a guess.
- A task the engine cannot run faithfully — multi-container, multi-step,
  non-Linux, or an instance whose log parser is not implemented — is
  `UNSUPPORTED` and excluded from totals, never run anyway.
- Build failures, timeouts and unreadable scores are their own category, never
  folded into a pass or a failure.

A task shipping no reference solution is `NO_ORACLE` rather than an error:
upstream documents `solution/` as optional, so it limits what can be verified
rather than indicating a defect.

### Known limitations

- The `partial` control needs a patch with at least two hunks, which is 220 of
  the 500 SWE-bench Verified instances (44%). It does not apply to shell-script
  solutions at all, so it reaches none of the Harbor corpus.
- Multi-container compose tasks are reported `UNSUPPORTED` rather than
  orchestrated.
- SWE-bench exports are read as JSONL or JSON, not parquet.
- Published SWE-bench images are ~4 GB each and x86_64, so a large run needs
  substantial disk and, on arm64 hosts, emulation.

[Unreleased]: https://github.com/bugyal/skeptic/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/bugyal/skeptic/releases/tag/v0.1.0
