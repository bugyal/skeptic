# Partial-control fixture

A synthetic SWE-bench-shaped instance whose test suite is deliberately weak.

`calc.py` ships two bugs. The reference patch fixes both, in two separate
hunks. The test suite only exercises `add`, never `sub`.

Both original controls therefore call the task clean — an untouched workspace
scores 0.0 and the full reference patch scores 1.0. Only the partial control
sees the problem: withholding the `sub` hunk still scores 1.0, because nothing
grades it.

This is the weak-test failure mode in miniature, the one that accounted for
31% of apparently-passing SWE-bench patches.

The e2e test builds the image itself; it is not published anywhere.
