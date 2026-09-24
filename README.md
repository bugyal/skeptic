# skeptic

**Skeptic runs BOTH controls across EVERY task, classifies the outcome, captures evidence, and lints leaks statically — one binary, zero LLM calls.**

Skeptic doesn't believe your benchmark until oracle passes and nop fails.

A benchmark hands an AI coding agent a codebase and a task, runs hidden tests, and
prints a score. That score is only worth something if the task is fair. Skeptic runs
control experiments on every task in a benchmark and tells you which ones aren't.

```console
$ skeptic check ./tasks
VERDICT         TASK                             NOP  ORACLE  REASON
BOTH            skeptic-fixtures/both-wrong     0.50    0.50  nop 0.50, oracle 0.50
NOP_PASSES      skeptic-fixtures/nop-passes     1.00    1.00  tests pass without any change (nop 1.00)
ORACLE_FAILS    skeptic-fixtures/oracle-fails   0.00    0.00  reference solution does not pass (oracle 0.00)
ERROR           skeptic-fixtures/build-error       –       –  build failed: image build failed (exit 1)
NO_ORACLE       skeptic-fixtures/no-solution    0.00       –  no reference solution; oracle not run
CLEAN           skeptic-fixtures/clean          0.00    1.00  oracle 1.00, nop 0.00

1 clean · 3 flagged · 1 errors · 1 no-oracle
```

Exit status is non-zero when anything is flagged, so it drops into CI unchanged.
No API key. No model calls. One static binary.

## Why this exists

When researchers audited SWE-bench, of the patches the harness had marked as passing:

| Finding | Share |
| --- | --- |
| The fix was already in the issue text — the model transcribed it | **32.67%** |
| Tests too weak to tell a correct patch from an incorrect one | **31.08%** |

Removing those instances dropped SWE-Agent + GPT-4 from **12.47% to 3.97%**. Reported
performance was inflated roughly threefold.<sup>[1]</sup> Over 15% of SWE-bench
*Verified* instances still need test augmentation.<sup>[2]</sup>

Benchmarks are software, and software has bugs. Nobody checks the exam.

## Install

```sh
go install github.com/bugyal/skeptic/cmd/skeptic@latest
```

Or grab a binary from [Releases](https://github.com/bugyal/skeptic/releases).
Homebrew tap: planned.

Requires Docker (or any CLI-compatible runtime) for `check`. `lint` needs nothing.

## The two controls

Think of sanity-checking an exam. Hand in the official answer sheet: it must score
100%. Hand in a blank page: it must score 0%. Skeptic does exactly that to every task.
The **oracle** control applies the task's reference solution and requires a score of
1.0 — if the known-correct answer can't pass, no agent can. The **nop** control changes
nothing and requires a score of 0.0 — if an untouched workspace scores, the tests
aren't grading the change. Any task failing either control is flagged with its scores
and the captured output behind them.

## Verdicts

| Verdict | Meaning | Fails CI |
| --- | --- | :--: |
| `CLEAN` | oracle 1.0, nop 0.0 | |
| `NOP_PASSES` | tests reward an untouched workspace | ✓ |
| `ORACLE_FAILS` | the reference solution doesn't pass | ✓ |
| `BOTH` | both controls wrong | ✓ |
| `ERROR` | build failed, timed out, or score unreadable | ✓ |
| `NO_ORACLE` | no reference solution ships; oracle can't run | |
| `UNSUPPORTED` | recognised but not runnable faithfully | |

`ERROR` is never folded into a pass or a fail, and `UNSUPPORTED` is never guessed at.
A tool that claims your benchmark is lying cannot afford to invent a verdict of its own.

## Evidence

Every run writes per-task, per-control evidence — this is what you open when someone
disputes a flag.

```
.skeptic/runs/20260924-171524/
├── report.json                      versioned, diffable (docs/report-schema.json)
└── skeptic-fixtures-nop-passes/
    ├── result.json
    ├── build.log
    ├── nop/{test.stdout,test.stderr,exit-code.txt,reward.txt}
    └── oracle/{solution.stdout,test.stdout,reward.txt,…}
```

`skeptic report <run-dir> --format md` re-renders any past run as markdown ready to
paste into an upstream issue.

## Commands

```sh
skeptic check ./tasks           # run both controls over a task set
skeptic check ./tasks/one-task  # or a single task
skeptic lint  ./tasks           # static checks; no Docker, no model, no cost
skeptic report .skeptic/runs/… --format md
skeptic version
```

Useful `check` flags: `--task ID` (repeatable), `--limit N`, `--parallel N`,
`--timeout 30m`, `--only nop|oracle`, `--json out.json`, `--no-fail-on-flagged`,
`--keep-containers`, `--no-cache`.

### `skeptic lint`

Static structural checks, free and deterministic. The one that matters most is
**leakage**: whether the answer is reachable by the agent, either from the instruction
text (links to the PR that fixed it, an embedded diff) or from its own filesystem
(`solution/` or `tests/` copied into the image by a `COPY . .`, unless `.dockerignore`
excludes them).

```console
$ skeptic lint ./tasks
FAIL  example/leaky-context
      FAIL  leakage     the solution directory is inside the Docker build context and is copied into the image; an agent could read it
WARN  example/leaky-instruction
      WARN  leakage     instruction links to a pull request, issue or commit (…/pull/4821); an agent that opens it may read the answer instead of solving the task
```

## Supported formats

| Format | Status |
| --- | --- |
| Harbor / Terminal-Bench 2.x (`task.toml`) | supported |
| SWE-bench (local JSONL + published images) | supported |
| Terminal-Bench 1.x (`task.yaml`) | supported |
| Custom `skeptic.toml` | planned |

### Terminal-Bench 1.x exit-status mapping

Terminal-Bench 1.x tasks carry no Harbor-style verifier: grading is `run-tests.sh`,
whose contract is to propagate pytest's exit status, and Terminal-Bench's own harness
treats 0 as passed. Skeptic therefore maps the test entrypoint's exit status per the
D2 fallback — **exit 0 → 1.0, any other exit → 0.0**. A task that needs a finer-grained
score than binary pass/fail cannot be graded this way and should ship a Harbor-style
verifier writing `reward.json`.

The Harbor adapter was written against the upstream sources, not against
documentation: reward resolution follows `src/harbor/verifier/verifier.py`
(`reward.json` before `reward.txt`), container paths follow
`src/harbor/models/trial/paths.py`, and solution execution follows
`src/harbor/agents/oracle.py`.

Adapters implement three methods — `Name`, `Detect`, `Load` — and everything
downstream is format-agnostic. See `internal/adapter/adapter.go`.

## Results on public benchmarks

### Three Terminal-Bench tasks ship the answer inside the image

`skeptic lint` scans all 241 Terminal-Bench 1.x tasks in **0.18 seconds** and
fails three of them: `cross-entropy-method`, `multistep-definite-integral` and
`play-lord`. Each builds with `COPY . /app` and no `.dockerignore`, so
`solution.sh` and `tests/` are copied into the image the agent works in.

Confirmed by building the image and looking, not by reading the Dockerfile:

```console
$ docker run --rm probe sh -c 'ls /app'
Dockerfile  docker-compose.yaml  run-tests.sh  solution.sh  task.yaml  tests

$ docker run --rm probe sh -c 'grep expected /app/tests/test_outputs.py'
    expected = sympy.E - 2
```

The agent can read the reference solution and the graded assertion, including
the expected value. It can pass without solving anything — and unlike a leaked
link, this needs no network, just `cat`.

Three tasks of 241 is 1.2%: a real, fixable defect in a few tasks, not an
indictment of the benchmark. Details and a draft report in
[`results/terminal-bench-1/2026-09-25`](results/terminal-bench-1/2026-09-25).

### Static scan coverage

| Corpus | Tasks | OK | WARN | FAIL |
|---|---:|---:|---:|---:|
| [Terminal-Bench 1.x](results/terminal-bench-1/2026-09-25) | 241 | 212 | 26 | **3** |
| [SWE-bench Verified](results/swe-bench-verified/2026-09-25-leakage) | 500 | 431 | 69 | 0 |
| Harbor examples | 58 | 31 | 27 | 0 |

Harbor scoring zero is the point: its format keeps `solution/` and `tests/`
outside the `environment/` build context, so the leak that hits three
Terminal-Bench 1.x tasks is structurally impossible there.

### Answer leakage in SWE-bench Verified

`skeptic lint` scans all 500 instances in **0.44 seconds** — no containers, no
model, no API key — and flags **69 (13.8%)** whose problem statement carries a
leakage signal.

One of them, `scikit-learn__scikit-learn-14710`, contains **the fix itself** —
a diff at the same file and hunk as the gold patch, differing only in an
equivalent guard (`hasattr(self, 'classes_')` vs `is_classifier(self)`).

Eight more link the **exact pull request that is their own gold patch**.
Verbatim, from `django__django-10097`:

```
Pull request: https://github.com/django/django/pull/10097
```

That PR is the answer. The task hands the agent a URL to it.

This is the failure mode an [audit of SWE-bench](https://arxiv.org/html/2410.06992v1)
measured at 32.67% of apparently-successful patches — and it is detectable
statically, for free, in under a second.

The honest scope: this shows **the answer is reachable from the question**, not
that any model exploited it. The official harness runs without network access,
and a linked PR is not the same as a pasted fix. Full findings, the other 61
flagged instances, and the caveats are in
[`results/swe-bench-verified/2026-09-25-leakage`](results/swe-bench-verified/2026-09-25-leakage).

### Control runs

| Benchmark | Date | Instances | Clean | Flagged | Errors |
|---|---|---:|---:|---:|---:|
| [SWE-bench Verified](results/swe-bench-verified/2026-09-25-controls) | 2026-09-25 | 12 | 12 | 0 | 0 |

One instance per repository — the smallest subset exercising **all twelve** of
SWE-bench's log parsers, which between them cover all 500 instances. Every one
produced the expected split: empty diff 0.00, gold patch 1.00. Nothing was
flagged, reported as plainly as a failure would be.

The partial control ran on the one multi-hunk patch in the set
(`mwaskom__seaborn-3187`). Withholding either hunk drops exactly one
FAIL_TO_PASS test while all 248 regression tests hold — each hunk has its own
test, so that suite grades the whole change. A negative result, and a useful
one: the probe discriminates rather than flagging whatever it can reduce.

**This validates the adapter, not the benchmark.** Twelve instances of 500. The
full set needs roughly 2 TB of image traffic. Findings about SWE-bench itself
come from the static scan above, which covers all 500.

## In CI

```yaml
- uses: actions/checkout@v4
- uses: actions/setup-go@v5
  with: { go-version: '1.25' }
- run: go install github.com/bugyal/skeptic/cmd/skeptic@latest
- run: skeptic lint ./tasks           # fast, no Docker
- run: skeptic check ./tasks --json skeptic-report.json
- uses: actions/upload-artifact@v4
  if: always()
  with: { name: skeptic-report, path: skeptic-report.json }
```

## Prior art, honestly

[Harbor](https://github.com/harbor-framework/harbor) ships both controls as agents
(`src/harbor/agents/nop.py`, `src/harbor/agents/oracle.py`) and its docs recommend
running the oracle one while authoring a task. `harbor check` runs a 12-criterion
quality rubric that overlaps some of Skeptic's lint checks.

Skeptic is not a replacement for it. The difference is scope and cost: Harbor checks
one task at a time during authoring, and its quality checker is an LLM judge that
costs money per task and can disagree with itself between runs. Skeptic sweeps a whole
task set, classifies every task against both controls, does its structural checks by
parsing rather than prompting, and ships as a single binary with no Python environment
— the shape you can actually put in CI. It also targets formats Harbor doesn't.

Skeptic does not mine tasks or build benchmarks; tools like RepoBench and RepoTrials do
that. It does not run agents or call any model.

## Roadmap

- Run the full SWE-bench Verified set on a machine with the disk for it
- Exercise the remaining eight log parsers against real instances
- Multi-service compose orchestration (a whole compose stack brought up and
  cross-probed, beyond D4's single-buildable-service policy)
- Custom `skeptic.toml` for home-grown benchmarks
- `--repeat N` for flake detection — SWE-bench itself runs tests 3× and discards
  inconsistent ones
- `skeptic diff` between two runs, to catch benchmark rot over time
- `skeptic sweep` as an alias for `check`, per the original naming
- Homebrew tap

See [docs/decisions.md](docs/decisions.md) for why things are the way they are.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md). Found a broken task with Skeptic? There's an
[issue template](.github/ISSUE_TEMPLATE/broken-task.md) for reporting it upstream.

## License

MIT — see [LICENSE](LICENSE).

---

<sub>[1] Aleithan et al., *SWE-Bench+: Enhanced Coding Benchmark for LLMs*, [arXiv:2410.06992](https://arxiv.org/html/2410.06992v1).<br>
[2] [SWE-Bench deep dive: unmasking the limitations of a popular benchmark](https://runloop.ai/blog/swe-bench-deep-dive-unmasking-the-limitations-of-a-popular-benchmark).</sub>
