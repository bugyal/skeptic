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
| WARN | 8 |
| **FAIL** | **21** |

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

`cross-entropy-method` additionally copies in a directory named
`evaluation_tests_hidden`.

No harness-side cleanup removes these before the agent runs. The `shutil.rmtree`
calls in `terminal_bench/` operate on host-side cache and run directories, not
on the container filesystem.

**This is stronger than the SWE-bench leakage finding.** That one needs network
access to follow a link. This one needs `cat`.

## Why the other 21 FAILs are not findings

18 of the 21 FAILs are `no test command`, and every one of them is a task
Skeptic had already marked `UNSUPPORTED`. Those tasks use layouts the adapter
does not handle, so it never populated a test command, and lint then reported
the absence as a defect. **That is a bug in Skeptic, not in Terminal-Bench** —
lint should not re-report a task the adapter has already declined. Tracked and
fixed; see the commit following this one.

The remaining WARNs are 25 `solution` and 1 `leakage` (a task whose instruction
says "patch", which in context means `unittest.mock.patch`). Both are weak
signals, reported as warnings for a reason.

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
