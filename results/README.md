# Results

Every run Skeptic has done against a public benchmark, with the raw reports.
Each directory holds the machine-readable output and a README stating what was
found, what was not, and what the finding does not prove.

## Findings, strongest first

| Finding | Corpus | How it was verified | Exploitable by |
|---|---|---|---|
| [3 tasks ship `solution.sh` and `tests/` in the image](terminal-bench-1/2026-09-25) | Terminal-Bench 1.x | Built the image and read the files | `cat` — no network |
| [1 instance contains its gold patch as a diff](swe-bench-verified/2026-09-25-leakage) | SWE-bench Verified | Read the problem statement against the patch | reading the question |
| [8 instances link their own fixing PR](swe-bench-verified/2026-09-25-leakage) | SWE-bench Verified | Matched linked PR number to instance id | following a link |

## Runs

| Date | Benchmark | Kind | Outcome |
|---|---|---|---|
| [2026-09-25](swe-bench-verified/2026-09-25-controls) | SWE-bench Verified | controls | 12 of 12 parsers verified, all CLEAN |
| [2026-09-25](swe-bench-verified/2026-09-25-leakage) | SWE-bench Verified | static scan | 500 scanned, 69 flagged |
| [2026-09-25](terminal-bench-1/2026-09-25) | Terminal-Bench 1.x | static scan | 241 scanned, **3 failed** |
| [2026-09-24](swe-bench-verified/2026-09-24) | SWE-bench Verified | controls | superseded — 8 errored on a full disk |

Harbor's 58 example tasks were also scanned: **0 failures**. That is the point
rather than an absence of one — the Harbor format keeps `solution/` and `tests/`
outside the `environment/` build context, so the defect found in three
Terminal-Bench 1.x tasks is structurally impossible there. The older format made
the task root the build context; the newer one fixed it.

## How to read these

Three habits, because the whole tool is worthless if its findings cannot be
trusted:

**A flag is a signal, not a proof.** Six SWE-bench instances tripped the
"contains a diff" check and exactly one holds up. The other five overlap the
gold patch on generic lines like `try:` and `return True`. That ratio is in the
write-up next to the headline, not hidden below it.

**Errors are never findings.** The 2026-09-24 run has eight `ERROR` instances.
Every one was a host running out of disk, and one of them scored `CLEAN` on its
own minutes earlier. They are recorded as what they were.

**Reachable is not the same as exploited.** None of these runs show that a model
used any of this. Showing that needs an ablation, which has not been run.

## Draft reports

Reports written for upstream maintainers live under `issues/` and are **not
filed**. Read the evidence before sending anything, and keep the criticism on
the artefact rather than its authors.

- [SWE-bench: self-referential PR links](swe-bench-verified/2026-09-25-leakage/issues/)
- [Terminal-Bench: tests baked into the image](terminal-bench-1/2026-09-25/issues/) — **filed** as [terminal-bench-1#1474](https://github.com/harbor-framework/terminal-bench-1/issues/1474)
