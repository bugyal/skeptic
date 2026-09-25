# Weak tests in SWE-bench Verified

Two confirmed instances where the graded tests do not cover the reference
solution. Both are checkable without running Skeptic.

---

# 1. `matplotlib__matplotlib-24637` — a matched pair, half tested

The clearest case, and the one the partial control pointed at directly.

The gold patch adds two lines to `AnnotationBbox.draw`:

```diff
+        renderer.open_group(self.__class__.__name__, gid=self.get_gid())
         self.update_positions(renderer)
         ...
         self.offsetbox.draw(renderer)
+        renderer.close_group(self.__class__.__name__)
```

`open_group` and `close_group` are a matched pair: every opened SVG group must
be closed, or the output is malformed.

The instance is graded by one test,
`test_backend_svg.py::test_annotationbbox_gid`, which checks that the `gid`
appears in the rendered SVG.

| Withheld | Score | Graded? |
|---|---|---|
| `open_group` line | 0.00 (F2P 0/1) | yes |
| `close_group` line | **1.00 (F2P 1/1, P2P 29/29)** | **no** |

The test verifies the group is *opened* with the right gid. Nothing verifies it
is *closed*. An agent that adds only the `open_group` call scores 1.0 and
resolves the instance, while emitting SVG with an unbalanced group — the precise
defect the second line exists to prevent.

## Reproducing

```sh
skeptic check verified.jsonl --task matplotlib__matplotlib-24637 --partial
```

Or read it directly: the patch is two lines, and the test asserts on gid
presence only.

---

# 2. `sympy__sympy-13878` — eleven implementations, one test

Found after six false positives, and reached indirectly. It is checkable
without running Skeptic at all.

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


---

# Also flagged, not claimed

`sphinx-doc__sphinx-9229` withheld a real behavioural change in
`ClassDocumenter.get_doc` — returning `[]` rather than `None` when a variable
comment exists — and scored F2P 1/1. The companion hunk, which adds
`get_variable_comment()` and changes `add_content`, was correctly graded
(F2P 0/1).

It may well be a third weak test, but the `get_doc` branch could equally be
defensive code that is redundant for the tested scenario. Distinguishing the
two needs reading sphinx's autodoc flow more closely than has been done here,
so it is recorded rather than claimed.

# Tally, stated plainly

Nine hunks flagged across the sampled instances. Two are confirmed weak tests,
one is unresolved, six were artifacts of Skeptic's own heuristics — comment
edits, dead-code removal, unused imports, and one biased sample. Each artifact
class has since been fixed or classified apart.

That is a low hit rate. Every flag has to be read by hand before it means
anything, which is why this directory contains the reasoning rather than a
count.
