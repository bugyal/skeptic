> **DRAFT — not filed.** For review before anything goes upstream.
> Target: `SWE-bench/SWE-bench` (the dataset), not any of the linked projects.
> Nothing here is a criticism of the instances' authors; these are artefacts of
> how issue text was harvested, not mistakes anyone made by hand.

**Title:** 8 SWE-bench Verified instances link the pull request that is their own gold patch

### Summary

In eight instances of `SWE-bench_Verified`, the `problem_statement` given to the
model contains a URL to the pull request whose diff is that instance's own
`patch` field. The instance number and the linked PR number match.

### Instances

| `instance_id` | URL present in `problem_statement` |
|---|---|
| `django__django-7530` | https://github.com/django/django/pull/7530 |
| `django__django-10097` | https://github.com/django/django/pull/10097 |
| `django__django-13023` | https://github.com/django/django/pull/13023 |
| `django__django-14122` | https://github.com/django/django/pull/14122 |
| `django__django-14315` | https://github.com/django/django/pull/14315 |
| `django__django-14404` | https://github.com/django/django/pull/14404 |
| `django__django-15569` | https://github.com/django/django/pull/15569 |
| `django__django-15863` | https://github.com/django/django/pull/15863 |

All eight are `django/django`. That is expected rather than suspicious: Django is
231 of the 500 Verified instances, and its workflow routinely posts the PR link
back onto the ticket, which is where these statements were harvested from.

### Example

`django__django-10097`, verbatim from `problem_statement`:

```
Pull request: https://github.com/django/django/pull/10097
```

`django/django#10097` is the change recorded in that instance's `patch` field.

### Why it may matter

The official harness runs without network access, so this is not directly
retrievable during an evaluation run there. Two reasons it still seems worth
recording:

1. **Harnesses differ.** Agent setups that allow browsing — a growing share —
   can fetch the linked diff. An instance that is solvable by following a link
   measures retrieval, not repair.
2. **A public PR link is a contamination signal.** It indicates the fix was
   public and discussed, which bears on whether a model has seen it in training.
   That holds regardless of network access at evaluation time.

This is the failure mode reported at 32.67% of apparently-successful patches in
[SWE-Bench+ (arXiv:2410.06992)](https://arxiv.org/html/2410.06992v1). These
eight are the narrow, unambiguous subset: the linked PR *is* the gold patch.

### What this report does not claim

- No evidence any model exploited these. That would need an ablation comparing
  scores with the URL stripped, which has not been run.
- 37 further instances link somewhere in their own repository without the
  numbers matching. Those are weaker and are deliberately excluded here.

### Possible remedies

Listed for discussion; the trade-offs belong to the maintainers.

1. Strip or mask same-repository PR/commit URLs in `problem_statement`. Cheap,
   but edits the artefact away from the issue text as it really appeared.
2. Keep the text and publish the flag as instance metadata, so evaluations can
   report scores with and without the affected instances.
3. Document it as a known property, leaving the choice to harness authors.

Option 2 preserves the data and lets consumers decide, but it is a judgement
call rather than an obvious fix.

### Reproducing

```sh
python -c "from datasets import load_dataset; \
  load_dataset('SWE-bench/SWE-bench_Verified', split='test').to_json('verified.jsonl')"

go install github.com/bugyal/skeptic/cmd/skeptic@latest
skeptic lint verified.jsonl --json > lint-report.json
```

Static only: no containers, no model, no API key. 500 instances in ~0.4s. It
flags 69 instances on leakage signals overall; the table above is the
self-referential subset, isolated by matching the linked PR number against the
instance id.

Full output and the analysis script are in
[`results/swe-bench-verified/2026-09-25-leakage`](..).
