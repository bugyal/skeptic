# skeptic

[![ci](https://github.com/bugyal/skeptic/actions/workflows/ci.yml/badge.svg)](https://github.com/bugyal/skeptic/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/bugyal/skeptic)](https://github.com/bugyal/skeptic/releases/latest)
[![go reference](https://pkg.go.dev/badge/github.com/bugyal/skeptic.svg)](https://pkg.go.dev/github.com/bugyal/skeptic)

**Skeptic doesn't believe your benchmark until oracle passes and nop fails.**

Both controls, every task, with the evidence — plus static leak detection.
One binary, zero LLM calls, no API key.

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
| `FLAKY` | scores differ between identical runs (`--repeat N`) | ✓ |
| `ERROR` | build failed, timed out, killed for memory, tests could not reach the network, or score unreadable | ✓ |
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

For a long sweep, point `report` at a directory of per-task reports and it merges
them — so a run still in progress is readable at any point, not only once it
finishes.

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
`--keep-containers`, `--no-cache`, `--override-cpus N`, `--override-memory-mb N`,
`--repeat N`.

`--repeat N` runs the nop and oracle controls N times each, every run in a
fresh container, and reports a task whose scores disagree as `FLAKY`, listing
every score. One run cannot see a task that passes seven times in ten, and the
run it happens to get is reported as the truth. A run that errors, is killed
for memory or loses the network makes the task `ERROR`, never `FLAKY`: a
disagreement the host caused is not the benchmark's.

A task's declared CPU and memory limits are applied to every control's
container as hard limits, as Harbor's Docker environment does:
`cpus`/`memory_mb` (and the legacy `memory = "2G"`) in Harbor's `task.toml`,
`deploy.resources.limits` in a Terminal-Bench 1.x compose file, and the same two
keys in `skeptic.toml`. A test killed at the limit is `ERROR`, not a score of
zero. The two override flags replace every task's limits.

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
| Custom `skeptic.toml` | supported — see below |

### Custom `skeptic.toml`

For a home-grown benchmark that does not use Harbor's layout: put a
`skeptic.toml` in each task directory. Paths are relative to that directory and
may not leave it.

```toml
format_version = 1

[task]
id          = "my-bench/fix-parser"   # optional; defaults to the directory name
instruction = "instruction.md"        # optional; read by the leakage checks

[environment]
dockerfile = "environment/Dockerfile" # or: image = "registry/name:tag"
context    = "environment"            # required with dockerfile
workdir    = "/app"                   # required; solution and tests run here
# platform = "linux/amd64"            # build_args, build_timeout_sec also accepted
# cpus = 2                            # optional hard limits; leave out for none
# memory_mb = 2048

[solution]
kind   = "script"                     # "script", "patch" or "none"
script = "solution/solve.sh"          # its directory is uploaded to /solution
# patch = "solution/fix.diff"         # for kind = "patch", applied in workdir
# env, timeout_sec                    # optional

[tests]
command = "sh /grader/run.sh"         # run in workdir, after the solution
dir     = "tests"                     # optional; copied in after the solution
mount   = "/grader"                   # required with dir
# env, timeout_sec                    # optional

[tests.score]
kind  = "reward_file"                 # or "exit_code": 0 is 1.0, anything else 0.0
paths = ["/logs/verifier/reward.json", "/logs/verifier/reward.txt"]  # tried in order
# reward_key = "reward"               # for a multi-key reward.json
```

The format is strict on purpose. An unknown key, a missing required field, or a
field that does not fit the chosen `kind` makes the task `UNSUPPORTED` with the
reason, rather than running it on a guess — a typo such as `workdri` is
reported, not silently dropped. `kind = "none"` has to be written out; it gives
the `NO_ORACLE` verdict. A `patch` solution also gets the partial control.
Working examples are in [`testdata/custom`](testdata/custom).

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

All runs and raw reports are indexed in [`results/`](results/).

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
| [SWE-bench Verified](results/swe-bench-verified/2026-09-25-batch60) | 2026-09-25 | 59 (stratified) | 59 | **0** | 0 |
| [SWE-bench Verified](results/swe-bench-verified/2026-09-25-controls) | 2026-09-25 | 12 (all parsers) | 12 | 0 | 0 |

**The two headline controls found nothing.** Across a sample stratified over all
twelve repositories, every gold patch scored 1.0 and every empty diff scored
0.0. That is the right answer for a benchmark curated to remove unsolvable and
trivially-solvable instances, and it is reported as plainly as a failure would
be.

The first run of that batch ended with five `ERROR` instances from Docker Hub
rate limiting. Re-run the next day, four scored CLEAN and the fifth was not
reached. Nothing was wrong with them — which is why `ERROR` is its own category
rather than a zero.

The weak-test probe flagged 11 instances and **2 held up** after reading each by
hand — [`matplotlib__matplotlib-24637`](results/swe-bench-verified/2026-09-25-weak-tests)
and `sympy__sympy-13878`. The other eight were gold patches bundling the fix
with changes no test can observe: comments, unused imports, dead code,
docstrings, gallery scripts.

Take the precision seriously: **2 of 11**, and only because every flag was
verified individually. The probe points at things worth reading, not at
conclusions — and `docs/decisions.md` D13 explains why that ratio is close to
the technique's ceiling rather than a bug awaiting a fix.

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
- `skeptic diff` between two runs, to catch benchmark rot over time
- `skeptic sweep` as an alias for `check`, per the original naming
- Homebrew tap

See [docs/decisions.md](docs/decisions.md) for why things are the way they are.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md). Found a broken task with Skeptic? There's an
[issue template](.github/ISSUE_TEMPLATE/broken-task.md) for reporting it upstream.

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

---

<sub>[1] Aleithan et al., *SWE-Bench+: Enhanced Coding Benchmark for LLMs*, [arXiv:2410.06992](https://arxiv.org/html/2410.06992v1).<br>
[2] [SWE-Bench deep dive: unmasking the limitations of a popular benchmark](https://runloop.ai/blog/swe-bench-deep-dive-unmasking-the-limitations-of-a-popular-benchmark).</sub>
