# A weak test in SWE-bench Verified — `sympy__sympy-13878`

The first genuine weak-test finding, after six false positives. It is
checkable without running Skeptic at all.

## The claim

The gold patch adds **11 `_cdf` methods** to `sympy/stats/crv_types.py`,
across distributions including `ArcsinDistribution`, `GammaInverseDistribution`,
`KumaraswamyDistribution`, `QuadraticUDistribution` and
`TrapezoidalDistribution`.

The instance is graded by **one** fail-to-pass test:

```
FAIL_TO_PASS: ["test_arcsin"]
```

So of eleven new CDF implementations, exactly one is graded.

## Why the other tests do not cover it

`test_trapezoidal` and `test_quadratic_u` do appear in the instance's
`PASS_TO_PASS` list, which invites the assumption that those distributions are
covered. They are not. `PASS_TO_PASS` tests pass **before** the patch as well
as after — that is what the list means. A test that passed without the method
cannot be grading the method.

The remaining 17 `PASS_TO_PASS` entries are regression tests for unrelated
distributions (`test_benini`, `test_chi`, `test_gompertz`, `test_von_mises`…).

## What this means

An agent that implements `ArcsinDistribution._cdf` and nothing else scores
**1.0** and resolves the instance, while ignoring ten CDF implementations the
task's own reference solution treats as part of the fix.

This is the weak-test failure mode reported at 31.08% of apparently-successful
patches in [SWE-Bench+ (arXiv:2410.06992)](https://arxiv.org/html/2410.06992v1).

## How Skeptic surfaced it, and the honest caveat

The partial control withheld the hunk that adds `uppergamma` and `hyper` to the
module's import list — symbols used by two of the new CDFs — and the suite
still returned **F2P 1/1, P2P 19/19**.

That is an indirect route to the finding. The probe pointed at an import, not
at the ungraded CDFs; confirming what it meant took reading the patch and the
test lists by hand. The evidence above stands on its own and does not depend on
the probe being right.

Withholding the *real* fix hunk scored **0** (F2P 0/1), so the control did
separate the fix from the rest correctly.

## Reproducing

```sh
python -c "from datasets import load_dataset; \
  load_dataset('SWE-bench/SWE-bench_Verified', split='test').to_json('verified.jsonl')"

skeptic check verified.jsonl --task sympy__sympy-13878 --partial
```

Or check it with no tooling at all: count `def _cdf` in the instance's `patch`
field, then read its `FAIL_TO_PASS`.

## Scope

One instance. This says nothing about how many others are like it — answering
that needs the full 500-instance run. What it does show is that the failure
mode is present in SWE-bench Verified and is detectable.
