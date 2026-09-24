# Contributing

## Getting set up

```sh
git clone https://github.com/bugyal/skeptic && cd skeptic
make build
make test          # unit tests, fast, no Docker
make e2e           # end-to-end against real Docker
make lint          # gofmt, go vet, staticcheck
```

## The one rule

**Skeptic must never report a verdict it did not earn.**

Every change is measured against that. If the tool cannot check something
faithfully, the honest outcomes are `ERROR` or `UNSUPPORTED` — never a guess
dressed up as a result. A false `CLEAN` tells someone their broken benchmark is
fine; a false flag sends them chasing a task that was never broken. Both are
worse than saying "I don't know".

Concretely, this means:

- A score that cannot be parsed unambiguously is an error, not an average.
- A task the adapter cannot run is `UNSUPPORTED`, not run anyway.
- A build failure or timeout is its own category, never a zero.

## Adding an adapter

Implement `Name`, `Detect` and `Load` from `internal/adapter/adapter.go`, then
register it in `cmd/skeptic/main.go`. Everything downstream is format-agnostic.

Write the adapter against the upstream project's **source**, not its docs. The
existing Harbor adapter cites the exact files its behaviour is derived from; do
the same, so the next person can check your work. Where upstream is ambiguous,
record the decision in `docs/decisions.md` rather than burying it in code.

Add a fixture under `testdata/` for anything new, including the broken cases.

## Tests

- Unit tests must not need Docker. Fixtures live in `testdata/`.
- Anything needing a daemon goes in `e2e/` behind the `e2e` build tag.
- The report format is validated against `docs/report-schema.json`. Changing the
  shape means changing both, and bumping `SchemaVersion` if a consumer would break.

## Commits and PRs

Explain *why* in the commit body. Run `make lint test` before opening a PR.
