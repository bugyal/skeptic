# SWE-bench Verified — parallel re-run with the current build, 2026-09-26

The 12-instance controls subset again, now with `--parallel 3` and every fix
made since [`2026-09-26-native-repro`](../2026-09-26-native-repro), on the same
native x86_64 host.

```sh
skeptic check subset-instances.jsonl --task <id> --task <id> --task <id> --parallel 3 --partial
```

It ran in four batches of three, with images removed between batches to keep
disk bounded (about 12 GB peak, against 18 GB free).

## Result

```
$ skeptic diff ../2026-09-25-controls/report.json .
Could not compare — one side is ERROR or UNSUPPORTED; usually the machine, not the benchmark
  psf__requests-1921  CLEAN → ERROR  oracle scored 0.00, but its tests could not reach the network, ...

0 regressed · 0 fixed · 0 changed · 1 could not compare · 0 added · 0 removed · 11 unchanged
```

- **11 of 12 match the committed arm64 run exactly,** running three at a time.
  Parallelism changed no verdict.
- **`psf__requests-1921` is now `ERROR`, not `ORACLE_FAILS`.** Its tests need
  the internet, which this host's containers do not have. That is D16 working:
  the earlier native run, before the fix, flagged it as a broken benchmark.
- **`skeptic diff` files it under "could not compare" and exits 0,** which is
  D20 working. A move to `ERROR` is the machine until shown otherwise.
- **The partial probes on `mwaskom__seaborn-3187` match again to the test**:
  F2P 1/2 and P2P 248/248 for each withheld hunk.

## Found during the same validation

The run began as a request to validate everything with parallel Docker Compose.
The Compose half is recorded in `docs/decisions.md` D19:

- **`check` had never scored a Terminal-Bench 1.x task correctly,** and every
  reference solution scored 0. Fixed; the committed Terminal-Bench results are
  all from `lint` and were unaffected.
- **Compose applies the same CPU and memory limits Skeptic does,** measured on
  four compose files brought up in parallel. It also applies a memory
  reservation as a soft limit, which D17 had wrongly said was ignored.

## Files

| File | Contents |
|---|---|
| `batch1.json` … `batch4.json` | Schema-2 reports, three instances each. `skeptic report .` merges them. |
