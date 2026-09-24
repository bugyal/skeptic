---
name: Broken benchmark task
about: Report a task that Skeptic flagged, to the benchmark that owns it
title: '[broken task] <task id>'
labels: ''
assignees: ''
---

<!--
This template is for reporting a task UPSTREAM, to the benchmark that owns it.
Paste the output of:

    skeptic report .skeptic/runs/<timestamp> --format md

Please check the evidence yourself before filing. A flag is a strong signal,
not a proof, and the maintainers on the other end are doing you a favour by
reading it.
-->

**Task:** `<task id>`
**Verdict:** `<CLEAN | NOP_PASSES | ORACLE_FAILS | BOTH | ERROR>`
**Scores:** nop `<n>`, oracle `<n>`

### What this means

<!-- One of:
NOP_PASSES   — the tests score above zero against a completely untouched
               workspace, so they are not grading the change.
ORACLE_FAILS — applying the task's own reference solution does not score 1.0,
               so no agent could pass this task either.
BOTH         — both of the above.
ERROR        — the task could not be evaluated at all (build failure, timeout,
               unreadable score).
-->

### How to reproduce

```sh
go install github.com/skeptic-labs/skeptic/cmd/skeptic@latest
skeptic check <path-to-task> --no-fail-on-flagged
```

### Evidence

<details>
<summary>test output</summary>

```
<paste the last lines of test.stdout from the run directory>
```

</details>

### Environment

- skeptic version:
- OS / arch:
- Docker API version:
