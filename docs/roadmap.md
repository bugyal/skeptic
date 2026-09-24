# Roadmap

What is planned, and where to start if you want to help.

The five issues below are written to be picked up cold. Each names the files
involved, what "done" looks like, and the trap to avoid. They are ordered by how
useful they are, not by difficulty.

---

## 1. A custom `skeptic.toml` format — good first issue

**Why.** Anyone with a home-grown benchmark currently has to adopt Harbor's
layout to use Skeptic. A small declarative format would let them describe what
they already have. This is the most requested thing that does not exist yet.

**Shape.** One file per task directory:

```toml
[environment]
dockerfile = "Dockerfile"
context    = "."
workdir    = "/app"

[solution]
kind   = "script"          # or "patch"
script = "solution/fix.sh"

[tests]
command = "./tests/run.sh"
dir     = "tests"

[tests.score]
kind  = "reward_file"      # or "exit_code"
paths = ["/logs/reward.json", "/logs/reward.txt"]
```

**Where.** Add `internal/adapter/custom/`, implementing `Name`, `Detect` and
`Load` from `internal/adapter/adapter.go`; register it in
`cmd/skeptic/main.go`. `internal/adapter/harbor/harbor.go` is the model to
copy — it is the simplest existing adapter.

**Done when.** A fixture under `testdata/` loads, `skeptic check` runs it, unit
tests cover a valid and an invalid manifest, and the format is documented in the
README's Supported formats table.

**The trap.** Do not let an unknown or missing field quietly default to
something plausible. A manifest the adapter does not fully understand should
`Unsupported` the task. Guessing here produces a confident wrong verdict, which
is the one thing this tool must never do.

---

## 2. `--repeat N` for flake detection — good first issue

**Why.** A task that passes seven times in ten is broken, and one run cannot
see it. SWE-bench considers this real enough that its own pipeline runs each
test three times and discards any that is inconsistent.

**Where.** `internal/check/run.go` and `internal/check/parallel.go`. Control
runs are already independent and side-effect-free — each gets a fresh container
— so this is a loop around an existing call rather than a redesign.

**Done when.** `--repeat 3` runs each control three times, the report records
every score, and a task whose scores disagree is reported as flaky with the
spread. Add the count to `docs/report-schema.json` and bump the schema version
if the shape changes.

**The trap.** Decide deliberately what a flaky task's *verdict* is. A task
scoring 1.0, 1.0, 0.0 on oracle is not `CLEAN` and not quite `ORACLE_FAILS`
either. A new verdict is probably right; whatever you choose, say why in
`docs/decisions.md`.

---

## 3. `skeptic diff` between two runs

**Why.** Benchmarks rot. Base images drift, unpinned dependencies move,
network resources vanish. The interesting question in CI is usually not "is this
set clean" but "what changed since last time".

**Where.** A new `cmd/skeptic/diff.go`. The report schema is versioned and
stable precisely so this is possible; `internal/report/report.go` already has
`Load`.

**Done when.** `skeptic diff old.json new.json` prints tasks whose verdict
changed, in both directions, plus tasks that appeared or disappeared. Support
`--format table|md|json`. Non-zero exit when a task regressed.

**The trap.** A task moving to `ERROR` is not a regression in the benchmark —
it usually means the machine ran out of disk or the network faltered. Report
those in their own section rather than as new findings. This has already bitten
us once; see `results/swe-bench-verified/2026-09-24/README.md`.

---

## 4. Honour per-task resource limits — good first issue

**Why.** Harbor's `task.toml` declares `[environment] cpus` and `memory_mb`.
Skeptic parses both and then ignores them, so a task authored to run in 2 GB
gets whatever the host has. That makes results less reproducible than the task
author intended, and lets one task starve others under `--parallel`.

**Where.** `internal/adapter/harbor/harbor.go` parses them into its config
struct (`CPUs`, `MemoryMB`) but never puts them on the `task.Task`. Add fields
to `task.Environment` in `internal/task/task.go`, populate them in the adapter,
and pass them through in `internal/check/run.go`. The plumbing at the far end
already exists: `docker.StartOptions` has `Memory` and `CPUs`.

**Done when.** A task declaring `memory_mb = 2048` starts with `--memory 2048m`,
a unit test asserts the value reaches `StartOptions`, and a flag can override it.

**The trap.** An out-of-memory kill must not read as a failing test. Check
whether the container was OOM-killed and report `ERROR`, not a score of zero.

---

## 5. A Homebrew tap

**Why.** `brew install bugyal/tap/skeptic` is how most macOS users will
want this, and the README currently promises a tap that does not exist.

**Where.** A `bugyal/homebrew-tap` repository, plus a `brews:` block in
`.goreleaser.yaml`. goreleaser can push the formula on release.

**Done when.** The tap installs a working binary on both arm64 and amd64 macOS,
and the README's install section points at it instead of saying "planned".

**The trap.** The formula must not declare Docker as a dependency. `skeptic
lint` is useful with no container runtime at all, and forcing a Docker install
on someone who only wants the static checks is the wrong trade.

---

## Larger, not yet specified

- **Multi-service compose orchestration.** Today a task whose compose file
  declares more than one service is `UNSUPPORTED` (`docs/decisions.md` D4).
  Doing this properly means bringing up a stack, waiting on health checks, and
  deciding which service the controls act on.
- **Exercise the remaining log parsers against real instances.** Eight of the
  twelve SWE-bench parsers are ported and unit tested but have never run against
  a live instance. See `results/swe-bench-verified/2026-09-24/README.md`.
- **A full SWE-bench Verified run.** Needs roughly 2 TB of image traffic and a
  machine with real disk. The result belongs under `results/`.
- **Adapters for other task formats**, if and when ones with a reference
  solution and a machine-readable score appear. The adapter interface is three
  methods; the constraint is the format, not the plumbing.

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md). The one rule: Skeptic must never
report a verdict it did not earn. If the tool cannot check something faithfully,
`ERROR` and `UNSUPPORTED` are the honest answers.
