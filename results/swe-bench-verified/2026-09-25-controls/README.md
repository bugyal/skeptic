# SWE-bench Verified — control runs, 2026-09-25

**12 of 12 log parsers verified against real instances. All 12 CLEAN, no errors.**

This supersedes the partial run of [2026-09-24](../2026-09-24), where eight
instances errored because the host ran out of disk.

## Method

One instance per repository — the smallest subset exercising every log parser
SWE-bench uses. Run one at a time, removing each ~4 GB image before pulling the
next, so peak disk stays near one image rather than twelve.

```sh
skeptic check subset.jsonl --task <id> --partial
```

## Result

| Instance | Parser | Instances covered | nop | oracle |
|---|---|---:|---:|---:|
| `django__django-16082` | `parse_log_django` | 231 | 0.00 | 1.00 |
| `sympy__sympy-22914` | `parse_log_sympy` | 75 | 0.00 | 1.00 |
| `sphinx-doc__sphinx-8621` | `parse_log_sphinx` | 44 | 0.00 | 1.00 |
| `matplotlib__matplotlib-23314` | `parse_log_matplotlib` | 34 | 0.00 | 1.00 |
| `scikit-learn__scikit-learn-14141` | `parse_log_scikit` | 32 | 0.00 | 1.00 |
| `astropy__astropy-12907` | `parse_log_astropy` | 22 | 0.00 | 1.00 |
| `pydata__xarray-3677` | `parse_log_xarray` | 22 | 0.00 | 1.00 |
| `pytest-dev__pytest-6202` | `parse_log_pytest` | 19 | 0.00 | 1.00 |
| `pylint-dev__pylint-7080` | `parse_log_pylint` | 10 | 0.00 | 1.00 |
| `psf__requests-1921` | `parse_log_requests` | 8 | 0.00 | 1.00 |
| `mwaskom__seaborn-3187` | `parse_log_seaborn` | 2 | 0.00 | 1.00 |
| `pallets__flask-5014` | `parse_log_flask` | 1 | 0.00 | 1.00 |

Every instance produced the expected split: the empty diff scores 0.00, the
gold patch scores 1.00. Every parser SWE-bench Verified uses is now exercised
against a live container, covering all 500 instances by parser.

**No task was flagged.** On this subset, the controls found nothing wrong —
which is the result you want from a curated benchmark, and is reported as
plainly as a failure would be.

## The partial control, on real data

`mwaskom__seaborn-3187` is the only instance here with a multi-hunk patch, so it
is the only one where the weak-test probe applies. Both probes ran:

| Withheld hunk | Score | FAIL_TO_PASS | PASS_TO_PASS |
|---|---:|---|---|
| `seaborn/_core/scales.py` 378–391 | 0.00 | 1 of 2 | 248 / 248 |
| `seaborn/utils.py` 699–708 | 0.00 | 1 of 2 | 248 / 248 |

With the full patch, 2 of 2 FAIL_TO_PASS tests pass. Withhold either hunk and
exactly one drops out while all 248 regression tests hold — so **each hunk has
its own dedicated test**. This suite grades the whole change.

A negative result, and a useful one: it shows the probe discriminates rather
than flagging any patch it can reduce.

## Scope

This validates that Skeptic reads SWE-bench correctly. It is **not** an audit of
the benchmark: 12 instances of 500. The full set needs roughly 2 TB of image
traffic.

For findings about SWE-bench itself, see the static scan of all 500 instances in
[`../2026-09-25-leakage`](../2026-09-25-leakage).

## Cost

Measured on an arm64 host running the published x86_64 images under emulation:

| | |
|---|---|
| Per instance | 7–20 minutes |
| Image size | ~4 GB each |
| Peak disk with per-instance pruning | ~5 GB |
| Without pruning | 12 × 4 GB, which is what broke the 2026-09-24 run |

## Files

| File | Contents |
|---|---|
| `report.json` | Consolidated report, schema 1 |
| `subset-instances.jsonl` | The exact 12 instances used |
