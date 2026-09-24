# SWE-bench Verified — 2026-09-24

A partial run. Read the caveats before citing anything here.

## What was run

`SWE-bench/SWE-bench_Verified` (500 instances), exported to JSONL. Rather than
sampling at random, the subset takes **one instance per repository**, which is
the smallest set that exercises **all twelve log parsers**. The aim was to
validate the adapter across every parser, not to audit the benchmark.

## Result

| Instance | Parser | Instances that parser covers | nop | oracle | Verdict |
|---|---|---:|---:|---:|---|
| `django__django-16082` | `parse_log_django` | 231 | 0.00 | 1.00 | CLEAN |
| `sympy__sympy-22914` | `parse_log_sympy` | 75 | 0.00 | 1.00 | CLEAN |
| `astropy__astropy-12907`¹ | `parse_log_pytest_v2` | 98 | 0.00 | 1.00 | CLEAN |
| `pytest-dev__pytest-6202` | `parse_log_pytest` | 19 | 0.00 | 1.00 | CLEAN |

¹ From a separate single-instance run (`report-astropy-standalone.json`); it
errored in the batch for the infrastructure reason below. `parse_log_pytest_v2`
also serves scikit-learn and sphinx.

**Four parsers verified against real instances, covering 423 of 500.** Each
produced the expected split: the empty diff scores 0.00, the gold patch scores
1.00. No task was flagged.

## What did not run, and why

Eight instances reported `ERROR`. **None of them indicates a benchmark defect.**
Every one failed with a variant of:

```
write /var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db: input/output error
```

The host ran out of disk. SWE-bench evaluation images are **about 4 GB each**,
and twelve of them do not fit in the space that was free. `astropy__astropy-12907`
is the proof: it scored CLEAN on its own and errored in the batch.

This is the `ERROR` category doing its job. None of these were counted as passes
or failures, and none is reported as a finding. A tool that had scored them
zero would have declared eight working instances broken.

## Honest limits

- **Four of twelve parsers are exercised.** The other eight
  (`matplotlib`, `seaborn`, `xarray`, `pylint`, `requests`, `flask`, plus the
  scikit-learn and sphinx uses of `pytest_v2`) are ported from upstream and unit
  tested, but have not yet run against a real instance.
- **Four instances is not an audit.** Nothing here says anything about whether
  SWE-bench Verified is sound. It says the adapter reads it correctly.
- **The partial control contributed nothing to this run.** All four instances
  ship single-hunk patches, where withholding the only hunk just reproduces the
  nop control. It applies to 220 of 500 instances (see `docs/decisions.md` D10).

## Reproducing

```sh
python -c "from datasets import load_dataset; \
  load_dataset('SWE-bench/SWE-bench_Verified', split='test').to_json('verified.jsonl')"

skeptic check verified.jsonl --limit 5 --partial
```

Budget roughly **4 GB of disk and 7 minutes per instance**, and more on arm64,
where the published x86_64 images run under emulation. The full 500-instance set
needs about 2 TB of image traffic and is not a laptop job.

## Files

| File | Contents |
|---|---|
| `report-subset.json` | The 12-instance run, schema 1 |
| `report-astropy-standalone.json` | The separate astropy run |
| `subset-instances.jsonl` | The exact instances used |
