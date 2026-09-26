# SWE-bench Verified — native x86_64 reproduction, 2026-09-26

**11 of 12 reproduce exactly. The twelfth is this machine's network, not the
benchmark, and it exposes a gap in Skeptic.**

## Why

Every earlier SWE-bench run was on an arm64 Mac, running the amd64 images
under emulation. A result that only holds under emulation would be worth
knowing about. This re-runs [`2026-09-25-controls`](../2026-09-25-controls),
one instance per log parser, on a native x86_64 Linux host (Docker 29.3.1)
with the build of `cb2c124`.

```sh
skeptic check subset-instances.jsonl --task <id> --partial --json <id>.json
```

The same `subset-instances.jsonl`, one instance at a time, each image removed
before the next pull.

## Result

| Instance | 2026-09-25 (arm64, emulated) | 2026-09-26 (x86_64, native) |
|---|---|---|
| `astropy__astropy-12907` | CLEAN | CLEAN |
| `django__django-16082` | CLEAN | CLEAN |
| `matplotlib__matplotlib-23314` | CLEAN | CLEAN |
| `mwaskom__seaborn-3187` | CLEAN | CLEAN |
| `pallets__flask-5014` | CLEAN | CLEAN |
| `psf__requests-1921` | CLEAN | **ORACLE_FAILS** — see below |
| `pydata__xarray-3677` | CLEAN | CLEAN |
| `pylint-dev__pylint-7080` | CLEAN | CLEAN |
| `pytest-dev__pytest-6202` | CLEAN | CLEAN |
| `scikit-learn__scikit-learn-14141` | CLEAN | CLEAN |
| `sphinx-doc__sphinx-8621` | CLEAN | CLEAN |
| `sympy__sympy-22914` | CLEAN | CLEAN |

Every nop score is 0.00 and every oracle score 1.00 on both hosts, apart from
the one row discussed below.

The partial control on `mwaskom__seaborn-3187` reproduced down to test counts:

| Withheld hunk | Score | FAIL_TO_PASS | PASS_TO_PASS |
|---|---:|---|---|
| `seaborn/_core/scales.py` 378–391 | 0.00 | 1 of 2 | 248 / 248 |
| `seaborn/utils.py` 699–708 | 0.00 | 1 of 2 | 248 / 248 |

## `psf__requests-1921` is not a finding

The `requests` test suite makes live HTTPS calls. On this host every outbound
connection from a container goes through a TLS-intercepting proxy the
container does not trust, and some hosts are refused outright. The oracle log
shows it plainly:

```
requests.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain
FAILED test_requests.py::RequestsTestCase::test_HTTP_200_OK_HEAD - assert 403...
```

Among the failures is `test_DIGESTAUTH_WRONG_HTTP_401_GET`, one of the
instance's graded FAIL_TO_PASS tests, so the gold patch scored 0. With working
network, as on 2026-09-25, it scores 1. **The committed CLEAN stands.**

### What it says about Skeptic

Skeptic reported this as `ORACLE_FAILS`, a flag, when the honest verdict is
`ERROR`. It cannot currently tell "the reference solution fails" from "the
reference solution's tests could not reach the network", and on a host with
restricted egress it would report every network-dependent instance as a broken
benchmark. That is the failure `CONTRIBUTING.md` rules out. It is written up as
roadmap issue 6.

**Fixed since.** With the change recorded in `docs/decisions.md` D16, the same
instance on the same host now reports:

```
ERROR  psf__requests-1921  0.00  0.00  oracle scored 0.00, but its tests could not reach
       the network, so the score says nothing about the solution: E requests.exceptions.SSLError:
       [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain
```

That is the honest answer on this host. The reports in `reports/` are from
before the fix and are kept as they were written.

## Also reproduced on this host

- **The partial control's fixture** (`testdata/partial/demo`, D9): withholding
  the `add` hunk scores 0, withholding the `sub` hunk scores 1 and is flagged
  as `calc.py lines 4-5`. The fixture's `apk add` is blocked on this host, so
  the image was built on `python:3.12`, which already has the same tools; the
  code and tests were unchanged.
- **The Terminal-Bench 1.x scan**: see
  [`../../terminal-bench-1/2026-09-25`](../../terminal-bench-1/2026-09-25#reproduced-2026-09-26).

## Files

| File | Contents |
|---|---|
| `reports/<id>.json` | One schema-1 report per instance, as written by `--json` |
