# Terminal-Bench 1.x — static scan, 2026-09-25

All 241 tasks of `harbor-framework/terminal-bench-1`, scanned in **0.18 seconds**.
No containers, no model, no API key.

```sh
skeptic lint terminal-bench-1/original-tasks --json > lint-report.json
```

## Result

| Severity | Tasks |
|---|---:|
| OK | 212 |
| WARN | 26 |
| **FAIL** | **3** |

All three failures are the same defect, described below. Every other task is
clean or carries a warning.

## The finding: three tasks ship the answer inside the image

`cross-entropy-method`, `multistep-definite-integral` and `play-lord` each have a
Dockerfile that does `COPY . /app` (or `COPY . /app/`) with **no `.dockerignore`**.
The build context is the task root, which contains `solution.sh` and `tests/`.

So both are copied into the image the agent works in.

This was confirmed by building the image and looking, not by reading the
Dockerfile and inferring. For `multistep-definite-integral`:

```console
$ docker run --rm tb1-leak-probe sh -c 'ls /app'
Dockerfile
docker-compose.yaml
run-tests.sh
solution.sh          <- the reference answer
task.yaml
tests                <- the hidden test suite

$ docker run --rm tb1-leak-probe sh -c 'grep expected /app/tests/test_outputs.py'
    expected = sympy.E - 2
```

An agent in that container can read the reference solution and the graded
assertion, including the literal expected value. It does not have to solve the
problem; it can write the expected answer to the output file.

`cross-entropy-method` is the sharpest case. Its build context holds a directory
the author named **`evaluation_tests_hidden`** — the name states the intent —
and `COPY . /app` copies it in whole:

```console
$ docker run --rm probe sh -c 'ls /app/evaluation_tests_hidden'
test_q1_pointenv_step.py
test_q2_cross_entropy_optimize.py
test_q3_evaluate_plans.py
test_q4_caching_performance.py
```

The directory is not hidden. This is worth stating carefully: the author clearly
meant for these to be out of reach, and the `COPY . /app` line quietly undoes
that. It is exactly the kind of gap a static check catches and a human reading
the same Dockerfile does not.

No harness-side cleanup removes these before the agent runs. The `shutil.rmtree`
calls in `terminal_bench/` operate on host-side cache and run directories, not
on the container filesystem.

**This is stronger than the SWE-bench leakage finding.** That one needs network
access to follow a link. This one needs `cat`.

## A bug this scan exposed in Skeptic itself

The first run of this scan reported **21** failures, not 3. Eighteen of them
were `no test command` — and every one was a task Skeptic had already marked
`UNSUPPORTED`. The adapter stops populating a Task it has declined, and lint
then reported the resulting absence as a defect in the benchmark.

That is the same error as scoring a task the runner could not execute: blaming
the benchmark for Skeptic's own gap. Lint now stops after the instruction
checks once a task is `UNSUPPORTED`, and those 18 are warnings about Skeptic's
coverage rather than failures of the task.

The remaining warnings are 25 `solution` (no reference solution, so no oracle
control) and 1 `leakage` on a task whose instruction says "patch", which in
context means `unittest.mock.patch`. Weak signals, reported as warnings for a
reason.

## What this does not claim

- No evidence any agent exploited this. Showing that would mean running an agent
  with no tools but `cat`, which v0.1 does not do.
- Three tasks out of 241 is 1.2%. This is a real defect in a small number of
  tasks, not an indictment of the benchmark.
- Terminal-Bench 1.x is superseded by the Harbor format, whose layout keeps
  `solution/` and `tests/` outside the `environment/` build context by design —
  which is exactly the right fix, already made upstream.

## Files

| File | Contents |
|---|---|
| `lint-report.json` | Full output for all 241 tasks |
| `baked-in-tests.json` | The three affected task ids |
| `issues/` | Draft report, not filed |
