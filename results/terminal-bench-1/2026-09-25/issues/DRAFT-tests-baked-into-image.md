> **DRAFT — not filed.** For review first.
> Target: `harbor-framework/terminal-bench-1`.

**Title:** Three tasks copy `solution.sh` and `tests/` into the agent's image

### Summary

`cross-entropy-method`, `multistep-definite-integral` and `play-lord` have a
Dockerfile whose build context is the task root and which does `COPY . /app`,
with no `.dockerignore`. The task root holds `solution.sh` and `tests/`, so both
end up in the image the agent runs in.

### Reproducing

```console
$ docker build -t probe original-tasks/multistep-definite-integral
$ docker run --rm probe sh -c 'ls /app'
Dockerfile  docker-compose.yaml  run-tests.sh  solution.sh  task.yaml  tests

$ docker run --rm probe sh -c 'grep expected /app/tests/test_outputs.py'
    expected = sympy.E - 2
```

The reference solution and the graded assertion are both readable, so the task
can be passed by writing the expected value out rather than computing it.

`cross-entropy-method` is the clearest case: it has a directory named
`evaluation_tests_hidden`, and that is copied in too.

```console
$ docker run --rm probe sh -c 'ls /app/evaluation_tests_hidden'
test_q1_pointenv_step.py
test_q2_cross_entropy_optimize.py
test_q3_evaluate_plans.py
test_q4_caching_performance.py
```

The name records the intent, and the `COPY . /app` line undoes it silently.

I did not find harness-side cleanup that removes these before the agent runs —
the `shutil.rmtree` calls in `terminal_bench/` act on host cache and run
directories. If cleanup does happen somewhere I missed, this report is void and
I would be glad to be corrected.

### Affected

| Task | Dockerfile line |
|---|---|
| `cross-entropy-method` | `COPY . /app` |
| `multistep-definite-integral` | `COPY . /app` |
| `play-lord` | `COPY . /app/` |

### Suggested fix

Either add a `.dockerignore` to those task directories:

```
solution.sh
tests/
evaluation_tests_hidden/
```

or narrow the `COPY` to the files the task actually needs at build time. The
Harbor task format solves this structurally by keeping `solution/` and `tests/`
outside `environment/`, which is the build context.

### Scope

Three tasks of 241. Not an indictment of the benchmark — a small, fixable defect
in a few tasks, found by a static check.

### Found with

```sh
skeptic lint original-tasks
```

Static: no containers, no model. 241 tasks in 0.18s.
