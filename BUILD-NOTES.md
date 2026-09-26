# BUILD-NOTES

Status: **historical** — this records v0.1 as first built, on 2026-09-24.
For what has changed since, see `CHANGELOG.md` (0.1.1 through 0.2.0); for why,
`docs/decisions.md` (D1–D20); for what is left, `docs/roadmap.md`.
Everything below was verified with the exact commands listed at the end.

## What is implemented

- **`skeptic check`** — builds each task's environment, runs both controls
  (oracle: apply the reference solution, must score 1.0; nop: change nothing,
  must score 0.0), classifies the verdict, and writes per-task/per-control
  evidence under `.skeptic/runs/<timestamp>/<task>/`. Flags: `--task`, `--limit`,
  `--parallel N`, `--timeout`, `--only nop|oracle`, `--json`, `--no-fail-on-flagged`,
  `--keep-containers`, `--no-cache`, `--platform`, and **`--partial`** (below).
  Exit status is non-zero when any task is flagged.
- **`skeptic lint`** — deterministic static checks, no Docker: manifest parses,
  referenced files exist, and the answer is not reachable by the agent — from
  instruction text (PR/issue/diff links), from the build context (`solution/` or
  `tests/` copied in unless `.dockerignore` excludes them), from **build steps**
  (`RUN echo 42 > /app/answer.txt`, heredoc writes), and from **ENV pairs**
  carrying the answer. Dockerfile parsing is a real parser (line continuations,
  heredocs incl. quoted/dash delimiters, JSON forms, shell quoting), not regex.
- **`skeptic report <run-dir>`** — re-renders any past run as a table or markdown
  ready to paste into an upstream issue.
- **Verdict taxonomy** exactly as D2/D3 define: CLEAN, NOP_PASSES, ORACLE_FAILS,
  BOTH, ERROR, NO_ORACLE, UNSUPPORTED; `--fail-on-flagged` covers
  {NOP_PASSES, ORACLE_FAILS, BOTH, ERROR}. NO_ORACLE and UNSUPPORTED never trip it.
- **Reward reduction (D2, all rows)**: reward.txt float; reward.json scalar or
  single-key object or `reward` key; multi-key without `reward` → ERROR
  (`ErrAmbiguousReward`, sorted keys); missing both files → ERROR; NaN/Inf/
  out-of-range → ERROR. Refuse-to-guess throughout.
- **Partial control** (`check --partial`) — withholds one hunk of the reference
  patch and re-runs; a suite that still scores 1.0 never graded that hunk.
  Advisory only: reported as "weak-tested", never flags a task.
- **Compose policy (D4)** — a single service **with a build unit** is supported
  (Skeptic starts detached and overrides the command, which absorbs the
  Terminal-Bench `sleep infinity` boilerplate); a single service without a build
  unit is refused ("nothing to build or run"); >1 service is UNSUPPORTED with the
  service count; an unparsable compose file is refused because it may hide a
  second service.
- **Adapters**: Harbor/Terminal-Bench 2.x (`task.toml`), Terminal-Bench 1.x
  (`task.yaml` + `run-tests.sh`), SWE-bench (JSONL dataset export + published
  images). Registry keys on distinct markers; discovery is depth-bounded.
- **SWE-bench scoring** — a faithful port of upstream `grading.py`'s log parsers
  (pytest variants, django, sympy, astropy alias) and the binary resolve rule,
  including the two asymmetries: XFAIL passes, and SKIPPED is a regression for
  FAIL_TO_PASS but not for PASS_TO_PASS. Truncated parametrised ids resolve by
  prefix only when every candidate agrees; candidates are sorted first so the
  result never depends on Go's map ordering. An instance whose declared parser
  is not implemented is UNSUPPORTED, never scored with the wrong parser.

## Deviations from the original brief

- **`check`, not `sweep`.** The sweep command shipped as `skeptic check` with the
  same semantics (`--parallel` ≈ `--jobs`). The naming is on the roadmap as an
  alias; `skeptic` is also simply shorter to type in CI.
- **Evidence root is `.skeptic/runs/<timestamp>/`, not `runs/<timestamp>/`.** A
  dot-directory stays out of `git status` and out of any task-tree globbing;
  `--json` writes the run report wherever you point it.
- **`report` subcommand retained** alongside `check`/`lint` — it re-renders
  evidence without re-running anything.
- **Terminal-Bench 1.x solution staging.** The engine's script control copies
  `Solution.Dir` wholesale into `/solution`. TB 1.x ships `solution.sh` at the
  task root, so the adapter stages just that script into a scratch dir handed to
  the engine — pointing at the task root would copy `tests/` and the Dockerfile
  into the container, the exact leak the linter flags. Scratch dirs live for the
  process lifetime (KB-scale, immaterial next to image builds).
- **TB 1.x grading is the D2 exit-status fallback** (exit 0 → 1.0, else 0.0),
  documented in README; tasks needing finer scores should ship a verifier writing
  `reward.json`.
- **CI staticcheck runs once (on the 1.26 leg)** rather than per Go version — a
  finding does not depend on the toolchain, and the matrix stays fast.

## Known limitations

- **No multi-service compose orchestration.** D4 deliberately refuses stacks;
  bringing up a whole compose topology and cross-probing services is future work.
- **Verifier-protocol assumptions.** Harbor tasks are trusted to write
  `/logs/verifier/reward.{json,txt}`; a verifier that writes elsewhere is scored
  as if the file were missing (ERROR). SWE-bench instances are trusted to run
  their published eval script and to declare a log parser Skeptic implements.
- **Partial control covers patch solutions only** (single-hunk patches are
  skipped as there is nothing to withhold) and is advisory: a suite may honestly
  not be able to observe one hunk in isolation.
- **No `--repeat N` yet.** Flake detection (run tests N times, discard
  inconsistent results) is on the roadmap; SWE-bench itself does 3×.
- **The `nop` control trusts task.toml's claim that the agent starts from the
  built image as-is.** A task that mutates its own image at build time in ways
  the Dockerfile parse cannot see (e.g. `RUN` fetching a remote answer at
  build time) is caught only if the fetched value matches what solution/ writes.

## Verified with

```
gofmt -l .            # empty
go build ./...        # ok
go vet ./...          # ok
go test ./...         # ok — adapter, adapter/{harbor,swebench,tbench}, check,
                      #      dockerfile, lint, patch, report, swebench, task
./skeptic lint examples/clean         # OK   · 0 fail · 0 warn
./skeptic lint examples/no-oracle     # WARN · 0 fail · 0 warn (oracle absent by design)
./skeptic lint examples/honest-echo   # OK   · 0 fail · 0 warn
```

Docker-dependent paths (`check`, e2e) are unit-skipped without a daemon and
opt-in via `go test -tags e2e ./e2e/...`.