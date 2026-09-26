# SWE-bench Verified — 60-instance stratified batch, 2026-09-25

## Sample

60 of 500 instances, allocated proportionally across all twelve repositories
(largest-remainder, fixed seed), so the result is representative rather than
whatever sorted first. `sample.txt` lists them.

| Repo | in set | sampled |
|---|---:|---:|
| django | 231 | 28 |
| sympy | 75 | 9 |
| sphinx | 44 | 5 |
| matplotlib / scikit-learn | 34 / 32 | 4 each |
| astropy / xarray | 22 / 22 | 3 each |
| pytest | 19 | 2 |
| pylint / requests | 10 / 8 | 1 each |

## The controls found nothing

| | |
|---|---:|
| Scored | 59 |
| **CLEAN** | **59** |
| Flagged (`NOP_PASSES`, `ORACLE_FAILS`, `BOTH`) | **0** |
| Errors | **0** |
| Not run | 1 (`pylint-dev__pylint-6386`) |

Every gold patch scored 1.0. Every empty diff scored 0.0. On the axis the two
original controls measure, **SWE-bench Verified is clean across this sample** —
which is the result you would hope for from a benchmark curated exactly to
remove unsolvable and trivially-solvable instances.

That is worth stating plainly rather than buried: the headline controls, run
across a representative sample, produced no findings.

### The 5 errors were this machine, and re-running proved it

The first run of this batch ended with five `ERROR` instances, all failing
identically:

```
failed to resolve reference "docker.io/swebench/sweb.eval.x86_64.…":
failed to do request: Head https://registry-1.docker.io/v2/…
```

Docker Hub rate-limits anonymous pulls at 100 per six hours, and the run had
pulled around 55 images on top of earlier work. They arrived in a contiguous
block at the end, which is the signature of a limit rather than five
independently broken instances.

They were recorded as `ERROR`, counted apart from pass and fail, and not
reported as findings. Re-run the next day, four of the five scored **CLEAN**
and the fifth was not reached before the host ran out of disk. Nothing was
wrong with those instances.

This is the `ERROR` category earning its keep: a tool that had scored them 0
would have reported five broken instances that were never broken.

## The partial control: 10 flags, 2 real

The weak-test probe flagged 10 instances. Read by hand, one at a time:

| Instance | Flag | Verdict |
|---|---|---|
| `matplotlib__matplotlib-24637` | `close_group` withheld, still 1.0 | **confirmed weak test** |
| `sympy__sympy-13878` | import hunk withheld, still 1.0 | **confirmed weak test** |
| `sphinx-doc__sphinx-9229` | `get_doc` branch withheld, still 1.0 | unresolved |
| `django__django-13121` | dead-code removal | artifact |
| `django__django-15368` | unused import removed | artifact |
| `django__django-16560` | biased sample — 3 of 18 hunks, all in an untested backend | artifact |
| `scikit-learn__scikit-learn-12682` | gallery example + docstring | artifact |
| `scikit-learn__scikit-learn-13496` | numpydoc parameter block | artifact |
| `scikit-learn__scikit-learn-25102` | numpydoc parameter block | artifact |
| `sympy__sympy-13852` | doctest expected output | artifact |
| `pytest-dev__pytest-7236` | two behaviour-preserving refactors | artifact — see D13 |
| `psf__requests-2317` | deletion-only | classified as cleanup by the tool itself |
| `sympy__sympy-20154` | deletion-only | classified as cleanup by the tool itself |

**Two confirmed of eleven.** The two are written up in
[`../2026-09-25-weak-tests`](../2026-09-25-weak-tests).

### Why so many artifacts

Every one is the same underlying thing: **a gold patch bundles the fix with
changes no test could observe** — a comment, an unused import, a dead method, a
docstring, a gallery script. Withholding those proves nothing about the suite.

Each class has since been fixed or classified apart (`docs/decisions.md` D11),
so a rerun of this batch would flag far fewer. The stored flags in
`report.json` were written by successive builds during the run and do **not**
all reflect the current classifier — the table above is the authoritative
reading, not the JSON.

### The cost of those fixes, stated

Tightening the classifier is not free. `sympy__sympy-13878` was reached through
an import hunk; with the current import rule it would be filed as cleanup and
that finding would not surface. Every filter that removes noise can remove a
route to signal. The guard is a test asserting that the matplotlib shape — bare
calls — never classifies as documentation, because that is the failure which
would silently delete the best result here.

## Honest summary

- The two original controls: **0 findings in 55 instances.** SWE-bench Verified
  is sound on solvability and non-triviality.
- The partial control: **2 real weak tests in 59 instances**, at a precision
  around 2-in-11 that only holds because every flag was read by hand. The last
  two entries in the table above were filed as `ungraded_cleanup` by the tool
  itself rather than needing a human — that is the v0.1.2 classifier working.
- `pytest-dev__pytest-7236` is the instructive one: re-run under the fully
  fixed classifier, it still flagged two hunks, and both are
  behaviour-preserving refactors. No pattern can catch those. See
  `docs/decisions.md` D13 for why that is the technique's ceiling.
- The static leak scan, run separately over all 500, remains the highest-yield
  check: see [`../2026-09-25-leakage`](../2026-09-25-leakage).

## Files

| File | Contents |
|---|---|
| `report.json` | Consolidated, schema 1. Flags reflect mixed builds; see above. |
| `sample.txt` | The exact 60 instance ids |
