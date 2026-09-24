> **FILED** 2026-09-25 as
> [SWE-bench/SWE-bench#669](https://github.com/SWE-bench/SWE-bench/issues/669).
> Re-verified live against the dataset immediately before filing. Checked
> against #12 (hints_text leakage policy) and #465 (repo-state loopholes);
> this covers the gap between them, problem_statement itself.

### Summary

Nine instances in `SWE-bench_Verified` have their solution reachable from
`problem_statement` itself: one contains the gold patch as a diff, and eight
link the pull request that is their own gold patch.

This is adjacent to work already done here, and deliberately scoped to the gap
between it:

- **#12** established that `hints_text` is collected only from before the
  initial commit, because later comments may leak the fix. That filter is on
  `hints_text`; the links below are in `problem_statement`, which is derived
  from the issue body and so is not covered by it.
- **#465** covered agents reaching future repository state through `git`. This
  needs no repository access at all — the text handed to the model is enough.

I checked both before filing and do not believe this is a duplicate. Happy to be
told otherwise.

### 1. One instance contains its own fix

`scikit-learn__scikit-learn-14710`. Its `problem_statement` carries a unified
diff at the same file and hunk as the gold patch.

From `problem_statement`:

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

From the instance's `patch`:

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

Same file, same hunk at line 426, same added lines. The only difference is the
guard: `hasattr(self, 'classes_')` versus `is_classifier(self)`.

### 2. Eight instances link the PR that is their gold patch

The linked PR number equals the instance number in each case.

| `instance_id` | URL in `problem_statement` |
|---|---|
| `django__django-7530` | https://github.com/django/django/pull/7530 |
| `django__django-10097` | https://github.com/django/django/pull/10097 |
| `django__django-13023` | https://github.com/django/django/pull/13023 |
| `django__django-14122` | https://github.com/django/django/pull/14122 |
| `django__django-14315` | https://github.com/django/django/pull/14315 |
| `django__django-14404` | https://github.com/django/django/pull/14404 |
| `django__django-15569` | https://github.com/django/django/pull/15569 |
| `django__django-15863` | https://github.com/django/django/pull/15863 |

Verbatim from `django__django-10097`:

```
Pull request: https://github.com/django/django/pull/10097
```

All eight are `django/django`, which is expected rather than suspicious: Django
is 231 of the 500 Verified instances and its workflow routinely posts the PR
link onto the ticket these statements were harvested from.

A further 37 instances link somewhere in their own repository without the
numbers matching. Those are weaker and are not claimed here.

### What this does not claim

- **No evidence any model exploited this.** Demonstrating that needs an ablation
  comparing scores with the text stripped, which I have not run.
- The category-2 links are only fetchable where a harness allows network access;
  the official one does not. They remain a contamination signal regardless,
  since a linked PR indicates the fix was public and discussed.
- Category 1 needs nothing: the fix is in the text.

### Reproducing

```sh
python -c "from datasets import load_dataset; \
  load_dataset('SWE-bench/SWE-bench_Verified', split='test').to_json('verified.jsonl')"

go install github.com/bugyal/skeptic/cmd/skeptic@latest
skeptic lint verified.jsonl --json > lint-report.json
```

Static: no containers, no model, no API key. 500 instances in about 0.4s. It
flags 69 instances on leakage signals overall; the nine above are the subset
where the flag is unambiguous, isolated by matching linked PR numbers against
instance ids and by diffing `problem_statement` against `patch`.

Full output and analysis:
https://github.com/bugyal/skeptic/tree/main/results/swe-bench-verified/2026-09-25-leakage

A caution on the other 60: they are weak signals, and most are not findings.
Six instances trip a "contains a diff" check and only the scikit-learn one holds
up — the rest overlap the gold patch on lines like `try:` and `return True`.

### Possible remedies

For discussion; the trade-offs are yours.

1. Strip or mask same-repository PR and commit URLs in `problem_statement`.
   Cheap, but edits the artefact away from the issue text as it appeared.
2. Keep the text and publish the flag as instance metadata, so evaluations can
   report scores with and without the affected instances.
3. Document it as a known property and leave the choice to harness authors.

Option 2 preserves the data and lets consumers decide, but that is a judgement
call rather than an obvious fix.

Verified against `SWE-bench/SWE-bench_Verified` on 2026-09-25.
