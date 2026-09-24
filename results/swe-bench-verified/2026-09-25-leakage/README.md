# SWE-bench Verified — instruction leakage scan, 2026-09-25

The first finding Skeptic produced on a real public benchmark.

Static only. No containers, no model, no API key. All 500 instances in
**0.44 seconds**.

```sh
skeptic lint verified.jsonl --json > lint-report.json
```

## Result

| | Instances |
|---|---:|
| Scanned | 500 |
| Clean | 431 |
| Flagged (WARN) | **69 (13.8%)** |

Breaking down the 69:

| Finding | Count |
|---|---:|
| Links a pull request, issue or commit | 48 |
| Mentions a patch | 22 |
| Contains what looks like a diff | 6 |

## The sharpest case: a task that contains its own fix

`scikit-learn__scikit-learn-14710` does not merely link the answer. Its
`problem_statement` contains a unified diff of the fix, at the same file and the
same hunk as the gold patch.

From the problem statement:

```diff
+        if hasattr(self, 'classes_'):
+            y_small_train = self.classes_[y_small_train.astype(int)]
         self.train_score_.append(
             self.scorer_(self, X_binned_small_train, y_small_train)
         )

         if self._use_validation_data:
+            if hasattr(self, 'classes_'):
+                y_val = self.classes_[y_val.astype(int)]
```

From the instance's `patch` field — the answer:

```diff
+        if is_classifier(self):
+            y_small_train = self.classes_[y_small_train.astype(int)]
         self.train_score_.append(
             self.scorer_(self, X_binned_small_train, y_small_train)
         )

         if self._use_validation_data:
+            if is_classifier(self):
+                y_val = self.classes_[y_val.astype(int)]
```

Same file, same hunk at line 426, same added lines. The sole difference is the
guard: `hasattr(self, 'classes_')` versus `is_classifier(self)`, which are
equivalent here.

This needs no network and no link-following. The fix is in the question. It is
the "solution leakage" category in its purest form.

**A caution on the other five diff-flagged instances.** Only this one holds up.
`django__django-13410` overlaps the gold patch on 5 of 6 added lines, but those
lines are `try:`, `return True`, `return False` — generic Python that matches by
chance. The remaining four contain a diff of the *broken* behaviour or of an
unrelated file, not the fix. Six instances tripped the check; one is a real
leak. That ratio is why the check is a WARN.

## The other sharp subset: tasks that link their own answer

Of those, **8 instances link to the exact pull request that is their own gold
patch** — the instance number and the linked PR number are the same:

| Instance | Link embedded in the problem statement |
|---|---|
| `django__django-7530` | https://github.com/django/django/pull/7530 |
| `django__django-10097` | https://github.com/django/django/pull/10097 |
| `django__django-13023` | https://github.com/django/django/pull/13023 |
| `django__django-14122` | https://github.com/django/django/pull/14122 |
| `django__django-14315` | https://github.com/django/django/pull/14315 |
| `django__django-14404` | https://github.com/django/django/pull/14404 |
| `django__django-15569` | https://github.com/django/django/pull/15569 |
| `django__django-15863` | https://github.com/django/django/pull/15863 |

Verbatim, from `django__django-10097`:

```
Pull request: https://github.com/django/django/pull/10097
```

That PR is the gold patch. The task hands the agent a URL to its own answer.

A further 37 instances link somewhere in their own repository without the
numbers matching; those are weaker signals and are not claimed here.

## What this does and does not show

**Does.** These instances carry a pointer to their own solution in the text the
agent is given. That is the failure mode the
[SWE-Bench+ audit](https://arxiv.org/html/2410.06992v1) measured at 32.67%, and
it is detectable statically, for free, in under a second.

**Does not.** It is not proof that any model exploited it:

- The official SWE-bench harness runs without network access, so the link is
  not fetchable *inside that harness*. Many agent setups do have network.
- A link is not the fix. Some of these describe the problem and merely cite
  where discussion happened.
- Leakage of this kind also matters through training data, not just retrieval:
  a linked PR is a strong hint that the fix is public and was likely memorised.

The honest claim is narrow: **the answer is reachable from the question**, and
nobody had checked. Whether that changed any score is a separate experiment.

## Two caveats on the checks themselves

- `mentions a patch` (22 instances) is the weakest signal here. In a Python
  repository "patch" often means `unittest.mock.patch`. Treat it as a prompt to
  look, not a finding.
- All 8 self-referential instances are Django, which is unsurprising: Django is
  231 of the 500 instances and its workflow routinely cites the PR on the
  ticket. It is a property of the project's habits, not of Django's code.

## Files

| File | Contents |
|---|---|
| `lint-report.json` | Full lint output for all 500 instances |
| `self-referential.json` | The 8 instances that link their own fix |

Reproduce with the export described in
[`../2026-09-24/README.md`](../2026-09-24/README.md).
