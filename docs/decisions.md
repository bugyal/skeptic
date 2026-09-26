# Decisions

Running log of design decisions. Each entry states what was decided, why, and
what evidence it rests on. Entries are append-only; supersede rather than edit.

---

## D1. What Skeptic adds over Harbor's existing tooling

**Status:** open — awaiting decision.

Before writing code I read the Harbor and Terminal-Bench sources rather than
working from the project brief's summary. The brief assumed the two control
experiments were unclaimed ground. They are not. This entry records exactly what
upstream already does, so the positioning is honest and the scope is chosen with
the overlap visible.

### What Harbor already ships

**Both controls exist as first-class agents.**

`src/harbor/agents/nop.py` — the nop control, in full:

```python
class NopAgent(BaseAgent):
    async def run(self, instruction, environment, context) -> None:
        pass
```

`src/harbor/agents/oracle.py` — `OracleAgent` uploads `solution/` to `/solution`,
chmods and runs the discovered `solve.{sh,bat}` with the task's `[solution].env`
layered above the trial env, captures stdout to `/logs/agent/oracle.txt`, and
writes `exit-code.txt` on non-zero exit.

**The oracle control is the documented authoring practice.**
`docs-mintlify/core-concepts/tasks/solution.mdx`:

> **Recommended** while authoring tasks — run `harbor run` with the Oracle agent
> to verify the task is solvable.

**A quality checker already flags answer leakage.**
`harbor check <task-dir>` runs 12 criteria from
`src/harbor/cli/quality_checker/default-rubric.toml`. Three overlap directly with
the `skeptic lint` checks in the brief:

| Harbor criterion | Overlapping lint check from the brief |
|---|---|
| `tests_or_solution_in_image` | solution/tests not inside the Docker build context |
| `hardcoded_solution` | solution that echoes the answer instead of deriving it |
| `anti_cheating_measures` | answer embedded in the environment |

### Where the overlap stops

**Harbor's checker is an LLM judge.** `QualityCheckResult` carries a `cost_usd`
field and the rubric is written as `guidance` prose for a model to weigh
(`src/harbor/cli/quality_checker/models.py`). It costs money per task, needs an
API key, and gives a different answer on a rerun. A static parse of the
Dockerfile's `COPY`/`ADD` lines and `.dockerignore` answers
`tests_or_solution_in_image` deterministically, for free, with no key — which is
the only form that belongs in CI. The brief's v0.1 rule that Skeptic makes zero
LLM calls is what makes this a real distinction rather than a reimplementation.

**Nothing upstream runs both controls across a whole task set and classifies the
result.** `harbor run` executes one agent over a dataset and reports rewards.
Deciding that oracle=1.0 *and* nop=0.0 must both hold, and naming the four ways
that fails, is the part with no upstream equivalent.

**Nothing reduces a multi-key reward to a verdict.** `src/harbor/models/job/result.py`
keeps `rewards` as `dict[str, float | int]` and reports statistics per key. There
is no upstream scalar. Skeptic needs a reduction rule and must not invent one
silently (see D2).

**Harbor is a Python package.** The brief's target user wants one static binary in
CI with no Python environment. That is a distribution claim, not a capability
claim, and it stands on its own.

### Honest one-line summary

Harbor can already run either control on one task, and tells authors to run the
oracle one. Skeptic's contribution is running **both** across **every** task,
classifying the outcome, proving it with captured evidence, doing the leak checks
statically instead of with a paid model, and covering formats Harbor does not.

That is narrower than "nobody checks this" — and still worth building. But the
README must not imply the controls are novel.

### Options

- **A — Keep scope, sharpen the pitch.** Build as specced. README credits
  Harbor's `nop`/`oracle` agents and `harbor check` by name, and positions
  Skeptic as the sweep + verdict + evidence + static-lint + multi-format layer.
- **B — Lead with lint and cross-format.** Re-weight toward what has no upstream
  analogue: deterministic leak detection and SWE-bench/custom formats. The
  controls become one feature among several rather than the headline.
- **C — Something else** once the overlap is visible.

**Recommendation: A.** The pitch survives contact with the evidence — a
benchmark consumer downloading a public task set still has no way to ask "is all
of this fair?" in one command, and the per-task authoring workflow Harbor
documents does not answer that question at 241-task scale. B understates the
part users actually want.

---

## D2. Reducing a reward to a single score

**Status:** decided (revisit if a real task set disagrees).

`src/harbor/verifier/verifier.py` resolves a reward by checking
`/logs/verifier/reward.json` **first**, then `/logs/verifier/reward.txt`, and
raising if neither exists. `reward.json` may hold a bare scalar or an object of
named rewards; `reward.txt` holds one float and is normalised to
`{"reward": value}`. Harbor never collapses the object to one number.

Skeptic needs one number per control. The rule:

| Reward artifact | Score |
|---|---|
| `reward.txt` with a finite float | that value |
| `reward.json` holding a scalar | that value |
| `reward.json` object with one key | that value |
| `reward.json` object containing a `reward` key | that key's value |
| `reward.json` object, several keys, no `reward` key | **`ERROR`**, listing the keys |
| neither file present | **`ERROR`** |

The last two cases deliberately refuse to guess. Averaging or taking the minimum
would turn an ambiguous artifact into a confident verdict, and a tool whose whole
claim is "this benchmark is lying to you" cannot itself guess and present the
guess as a finding. Non-finite and non-numeric values are `ERROR` too, matching
the upstream parser's rejection of `NaN`/`Infinity`.

---

## D3. A task with no reference solution is not an error

**Status:** decided.

`docs-mintlify/core-concepts/tasks/solution.mdx` states the `solution/` folder is
optional and that "Without `solution/`, the Oracle agent cannot run." This is a
supported configuration, not a defect: 6 of Harbor's 31 example tasks ship no
`solution/` directory.

The brief's taxonomy (`CLEAN` / `NOP_PASSES` / `ORACLE_FAILS` / `BOTH` / `ERROR`)
has no slot for it. Filing it as `ERROR` would inflate the error count with
working tasks; filing it as `CLEAN` would claim an oracle result that was never
obtained.

Added verdict: **`NO_ORACLE`** — the nop control still runs and is still reported,
the oracle control is recorded as not applicable, and the task is counted
separately in the summary line. It does not trip `--fail-on-flagged`, because
the task set author made a legitimate choice. `skeptic lint` warns about it,
since a benchmark whose tasks cannot be oracle-checked is weaker.

---

## D4. Docker Compose: support the boilerplate case, refuse the rest

**Status:** decided.

Terminal-Bench 1.x ships a `docker-compose.yaml` for all 241 tasks, but it is
generated boilerplate: one `client` service building the task's `Dockerfile`,
`sleep infinity` as its command, `TEST_DIR` in the environment, and log volume
mounts. Harbor uses compose for 4 of 31 example tasks, and those are genuinely
multi-container (`hello-mcp`, `network-policy-matrix`, `sidecar-artifacts`,
`environment-env-multi`).

Skeptic v0.1 builds the referenced Dockerfile directly and supplies the container
paths itself. When a compose file declares more than one service, or a service
Skeptic cannot reduce to a single build, the task is reported as
**`UNSUPPORTED`** and excluded from the totals.

Running a multi-container task as if it were single-container would produce a
confident, wrong verdict — the exact failure Skeptic exists to prevent. Refusing
loudly is correct; full compose orchestration is a roadmap item.

---

## D5. A third control, and a reordered plan

**Status:** decided.

### Why a third control

An audit of SWE-bench ([SWE-Bench+, arXiv:2410.06992](https://arxiv.org/html/2410.06992v1))
found that among patches the harness marked as passing:

- **32.67%** were *solution leakage* — the fix was present in the issue text or
  its comments, so the model transcribed rather than solved.
- **31.08%** passed on *weak tests* — tests that could not distinguish a correct
  patch from an incorrect one.

Removing those instances dropped SWE-Agent+GPT-4 from **12.47% to 3.97%**.
Reported performance was inflated roughly threefold. Neither SWE-bench Lite nor
Verified addressed leakage, and [over 15% of Verified instances still require
test augmentation](https://runloop.ai/blog/swe-bench-deep-dive-unmasking-the-limitations-of-a-popular-benchmark).

The oracle and nop controls catch neither failure. On a leaked task the gold
patch still scores 1.0 and the empty diff still scores 0.0; both controls report
`CLEAN`. On a weak-tested task the same holds — a weak test still rejects an
empty diff. Weakness is only visible when the suite is shown a *wrong* answer,
which neither control ever produces.

So the two controls in the original brief address the third and fourth most
common documented failures while missing the first and second.

### The `partial` control

Apply the gold patch with one hunk withheld, then run the tests.

- Expected: score **< 1.0**. The withheld hunk was part of the required change,
  so a suite that grades the whole change must notice its absence.
- Observed 1.0: the withheld hunk is **untested**. Report `WEAK_TESTS`, naming
  the file and hunk that made no difference.

This is mutation testing aimed at the benchmark's own reference solution. It is
deterministic, needs no model and no API key, and costs one extra container run
per hunk sampled (capped, see below).

**Limits, stated plainly.** It requires a patch-shaped solution, because a hunk
is the unit being withheld. SWE-bench instances are patches without exception.
Harbor tasks are overwhelmingly `solve.sh` shell scripts — 1 of 31 example tasks
ships a `solution.patch` — and a shell script cannot be meaningfully reduced
without a model. The control therefore reports `NOT_APPLICABLE` on script
solutions rather than pretending to cover them.

Two further honest caveats: a single-hunk patch cannot be reduced at all, and
some hunks legitimately are not independently observable (a refactor split across
files). The control therefore emits **WARN, never FAIL**, and never trips
`--fail-on-flagged`. It reports evidence for a human to judge, which is the only
defensible posture for a heuristic.

To bound cost, Skeptic withholds hunks one at a time up to a cap
(`--partial-max-hunks`, default 3), chosen deterministically so reruns agree.

### Reordered phases

The original order reached SWE-bench last. But the documented breakage is
concentrated there, the `partial` control only works there, and a public results
table drawn from Terminal-Bench alone may be close to empty — a weak case for a
tool whose entire claim is that benchmarks are broken.

| Phase | Content |
|---|---|
| **1** | Core engine, `Task` model, adapter interface, Harbor/TB2 adapter, `check` / `lint` / `report`, fixtures, unit + e2e tests |
| **2** | SWE-bench adapter and the `partial` control |
| **3** | Public run against SWE-bench Verified; commit results; README table |
| **4** | Terminal-Bench 1.x adapter; public run against its 241 tasks |
| **5** | Release: goreleaser, CHANGELOG, roadmap |

Phase 1 keeps the Harbor adapter first despite the reordering. It is the simpler
of the two — local directories, a plain Dockerfile, a reward file on disk — so it
proves the engine end to end without also depending on a dataset download and
published images. The fixtures for every verdict class are written in that
format for the same reason.

### Deferred, with reasons

- **`--repeat N`** (flake detection). Real problem: SWE-bench runs each test
  three times and discards any that is inconsistent. Deferred to roadmap, not
  dropped. Control runs are independent by construction, so adding repetition
  later is a loop around an existing call rather than a redesign.
- **`skeptic diff`** (rot between runs). Roadmap, as the brief had it. The
  versioned report schema is what makes it possible later; that ships in phase 1.

---

## D1 (resolved). Positioning

**Status:** decided — supersedes the open question in D1.

Lead with the evidence, credit Harbor by name. The README opens with the measured
failure rates rather than an assertion, presents the controls as one layer of an
integrity check rather than a novelty, and states plainly that Harbor ships
`nop` and `oracle` agents and that `harbor check` covers some of the same ground
with an LLM judge.

This is chosen over a pure controls-first pitch because the research showed the
controls cover the *less* common failures; a headline built on them alone would
oversell. It is chosen over a lint-first pitch because the sweep is still what a
benchmark consumer actually wants to run.

README copy is cheap to revise once real findings exist, so this is a starting
position, not a commitment.

---

## D6. Docker through the CLI, not the Go SDK

**Status:** decided.

The brief listed the Docker Go SDK as acceptable. The CLI is used instead: it
adds no dependencies, keeps the binary small, and works with any
CLI-compatible runtime such as podman. Every call goes through one `Client`
type, so switching to the SDK means replacing one file.

The cost is that `docker` must be on PATH. `skeptic lint` needs no runtime at
all, which is the path a CI job can always take.

---

## D7. SWE-bench exports are read as JSONL, not parquet

**Status:** decided.

A parquet reader would be the largest dependency in the project, for a format
users can convert in one line:

```python
load_dataset("SWE-bench/SWE-bench_Verified", split="test").to_json("verified.jsonl")
```

JSON arrays and newline-delimited JSON are both accepted.

The adapter reads the **augmented** datasets under the `SWE-bench` org, which
carry `image`, `eval_script`, `log_parser` and `eval_type` per row. The classic
`princeton-nlp/SWE-bench_Verified` columns do not include these, and an export
missing them is reported `UNSUPPORTED` with that reason rather than guessed at.

This is what makes the adapter tractable: because each row ships its own eval
script and names its own parser, Skeptic never reimplements the repository-to-
test-command mapping. It ports the parsers, which are finite (7 real
implementations, the rest aliases) and cover all 500 Verified instances.

---

## D8. Test output must be merged inside the container

**Status:** decided, after a bug.

SWE-bench eval scripts run under `set -uxo pipefail` and bracket the test run
with marker lines. Those markers are shell **xtrace output, on stderr**, while
test results go to **stdout**. Scoring means reading what falls between them.

Capturing the two streams separately and reassembling them on the host does not
work. They are separate pipes with no ordering guarantee, and `os/exec` copies
them on independent goroutines. In practice the fixture run failed roughly
three times in four: the results line landed outside the marker window and the
instance was reported `ERROR — no test results parsed from output`.

Test commands therefore run with stderr redirected into stdout **inside the
container** (`{ cmd ; } 2>&1`), which is the only way to get the order the
program actually wrote. The separate streams are still captured as evidence.

Worth recording because the failure mode was a plausible-looking wrong answer
rather than a crash: a benchmark would have been reported broken when the only
broken thing was Skeptic's own output handling.

---

## D9. The partial control, verified

**Status:** verified end to end.

`testdata/partial/demo` is a synthetic instance whose suite grades half of what
the task requires. The reference patch fixes two functions in two hunks; the
tests exercise only one of them.

Observed:

| Control | Score | Reading |
|---|---|---|
| nop | 0.00 | correct — an untouched workspace fails |
| oracle | 1.00 | correct — the full fix passes |
| partial, `add` hunk withheld | 0.00 | the suite noticed |
| partial, `sub` hunk withheld | **1.00** | **the suite never graded it** |

Both original controls classify this task `CLEAN`. Only the partial control
sees the problem. The discrimination matters as much as the detection: a probe
that flagged both hunks would be noise. `e2e/partial_test.go` asserts both.

---

## D10. How far the partial control actually reaches

**Status:** measured.

The control needs a patch with at least two hunks: withholding the only hunk of
a single-hunk patch reproduces the nop control, which has already run.

Measured across all 500 SWE-bench Verified instances:

| Patch shape | Instances | Partial control |
|---|---|---|
| Single hunk | 280 (56%) | not applicable |
| Two or more hunks | 220 (44%) | applies |

So the probe reaches a little under half of Verified, and none of the
Harbor/Terminal-Bench corpus, whose solutions are shell scripts.

This is worth stating plainly rather than burying: the control addresses the
weak-test failure mode, which accounts for about a third of apparently-passing
patches, but it can only speak about 44% of instances. It is a real signal over
a real subset, not a complete audit, and the README should say so.

Raising the reach would mean mutating solutions in ways that need a model to
stay plausible, which v0.1 rules out by design.

---

## D11. What the partial control actually finds on real patches

**Status:** observed, and it revises D5 downward.

D5 introduced the partial control on the strength of a synthetic fixture and a
published figure: withhold one hunk of the gold patch, and a score that stays at
1.0 means the suite never graded that hunk.

The fixture works. Real SWE-bench gold patches behave differently.

### Four flags, four artifacts

| Instance | Withheld hunk | Why the suite could not notice |
|---|---|---|
| `astropy__astropy-13398` | `siderial` → `sidereal` | a comment |
| `django__django-13121` | `date_interval_sql()` removed from 3 backends | dead code once another hunk landed |
| `django__django-15368` | `Expression` dropped from an import list | the import was left unused by the fix |
| `astropy__astropy-14182` | (scored by a build with the first bug) | unreliable, quarantined |

Not one was a weak test. All were the same phenomenon:

> **A gold patch bundles the fix with the cleanup the fix enables.**

The real change makes something redundant — a helper, an import, a comment —
and the same commit tidies it away. That tidying is unobservable to any test by
construction, so a suite awarding full marks without it is behaving correctly.

### What was done about it

`Hunk.Unobservable()` classifies three kinds: comments or blank lines only,
imports only, deletion only. Comment-only hunks are skipped; the others are
still probed and reported as `ungraded_cleanup`, kept apart from `weak_tests`.

They are classified rather than filtered on purpose. Deleting code that **is**
still called, with the suite not noticing, would be a real finding, and
suppressing the category outright would hide it.

### The honest limitation

Each fix narrows the false positives; none removes the underlying problem.
Skeptic cannot tell "this change is untested" from "this change is
unobservable" in general — that needs to know whether the withheld code is
reachable from the tests, which is program analysis, not diffing.

So the control's output is best read as: *these hunks were not graded*, with
most of them benign. That is a weaker claim than D5 made, and the README should
match it rather than the fixture.

### What still holds

The discrimination is real and worth keeping. On every instance where the fix
itself was withheld, the score dropped — `django__django-15368` went to
F2P 0/1, `mwaskom__seaborn-3187` to F2P 1/2, `astropy__astropy-13398` to
P2P 67/68. The control reliably separates the fix from the cleanup around it.
It is the label on the cleanup that was wrong, not the measurement.

---

## D12. The partial control, measured

**Status:** measured on a 60-instance stratified sample. Supersedes the
estimates in D5 and D11.

### Result

Ten flags. Two were real.

| Instance | Verdict |
|---|---|
| `matplotlib__matplotlib-24637` | **weak test** — `close_group` withheld, suite still 1.0 |
| `sympy__sympy-13878` | **weak test** — 11 `_cdf` methods, one graded test |
| `sphinx-doc__sphinx-9229` | unresolved |
| seven others | artifacts: comments, imports, dead code, docstrings, a gallery script, one biased sample |

Precision **2 of 10**, and only because every flag was read by hand. A user who
trusted the flag count would have chased eight non-problems.

The two original controls, over the same 55 scored instances, found **nothing**.
That is the expected result for a benchmark curated to remove unsolvable and
trivially-solvable instances, and it means the findings in this project all came
from checks added after the brief: the partial control and the static leak scan.

### Every filter that removes noise can remove signal

This is the part worth remembering. `sympy__sympy-13878` was reached through the
hunk that adds `uppergamma` and `hyper` to an import list — symbols the ungraded
CDFs need. After the import-continuation fix, that hunk classifies as cleanup,
so **the current build would not surface that finding**.

The fix is still right: seven artifacts against one indirect hit is the wrong
trade, and the finding survives because it was written down. But the cost is
real and belongs in the open, not in a commit message.

The general shape: a probe that flags a symptom can be silenced by a rule about
symptoms. The defence is not to avoid the rules — it is to name, in a test, the
specific signal each rule must not eat. `TestDocstringOnlyDoesNotSwallowBareCalls`
exists because the clearest finding here is a pair of bare calls, and a plausible
prose heuristic would have swallowed it.

### What the control is for

Not "this benchmark has weak tests". It is: *here are hunks of the reference
solution that the graded tests did not notice; most will be cleanup, read them.*

A reviewer pointed at ten hunks, two of which matter, is better served than one
pointed at nothing — but only if the output says which kind of claim it is
making. `weak_tests` versus `ungraded_cleanup` exists for that reason, and the
README says 2-of-10 rather than 10.

---

## D13. The partial control has a ceiling, and this is it

**Status:** observed. Bounds D12 rather than extending it.

`pytest-dev__pytest-7236` was re-run under the classifier with all five
artifact fixes in place. It still flagged two hunks, and both are artifacts of a
kind no pattern can catch.

The patch introduces a helper:

```python
def _is_skipped(obj) -> bool:
    """Return True if the given object has been marked with @unittest.skip"""
    return bool(getattr(obj, "__unittest_skip__", False))
```

and replaces three call sites. The third is the fix — adding
`and not _is_skipped(self.obj)` to a pdb teardown condition — and the control
graded it correctly (withholding it scored F2P 0/1).

The first two are:

```diff
-        skipped = getattr(cls, "__unittest_skip__", False)
+        skipped = _is_skipped(cls)

-        if getattr(self, "__unittest_skip__", None):
+        if _is_skipped(self):
```

Both are **behaviour-preserving refactors**: `_is_skipped(cls)` is by
construction the same truth value as the expression it replaces. No test can
distinguish them, because there is nothing to distinguish.

### Why this is the ceiling

The five fixes in v0.1.2 each recognise a *syntactic* signal — a comment, an
import, a deletion, a path, a documentation construct. A refactor has no
syntactic signal. Establishing that `getattr(x, "y", False)` and
`bool(getattr(x, "y", False))` are equivalent is semantic analysis, and doing it
in general is undecidable.

Gold patches contain refactors often, because the natural way to write a fix
that needs a predicate three times is to extract the predicate. So:

> The partial control's precision is bounded above by how frequently reference
> solutions bundle refactoring with their fix, and that bound cannot be raised
> by better pattern matching.

### What follows

Nothing to fix. Three things to say honestly:

1. The control is a **reading aid**, not a detector. Its output is "the graded
   tests did not notice these hunks", and the reader decides which of fix,
   cleanup and refactor each one is.
2. The 2-of-10 precision in D12 is not a bug awaiting a fix. It is close to what
   this technique can do on real gold patches.
3. Raising it would need a different technique — coverage instrumentation, to
   ask whether the withheld lines are executed by the graded tests at all.
   That is a real option and a much larger one; it is not v0.1.

---

## D14. Correction: the partial control's precision is 2 of 11

**Status:** correction. Supersedes the count, not the reasoning, in D12 and D13.

D12 reports ten flags. The batch's own verdict table
(`results/swe-bench-verified/2026-09-25-batch60/README.md`) lists eleven:
`pytest-dev__pytest-7236` was re-run under the finished classifier after D12 was
written, still flagged, and was added to the table without the count being
updated. `psf__requests-2317` and `sympy__sympy-20154` appear in the same table
but were filed as `ungraded_cleanup` by the tool, so they are not flags.

Precision is therefore **2 of 11**: two confirmed weak tests, one unresolved
(`sphinx-doc__sphinx-9229`), eight artifacts. The README already said 11; the
CHANGELOG and the batch README said 10 and now say 11. The conclusion in D13
does not change: the ratio is close to the technique's ceiling.

---

## D15. The `skeptic.toml` format is strict, and where it departs from the sketch

**Status:** decided.

The custom format is the only one with no upstream to read, so there is no
source of truth to derive defaults from. Every default would be Skeptic's
guess about someone else's benchmark. The adapter therefore makes the task
`UNSUPPORTED`, with the reason, whenever the manifest is anything short of
complete and unambiguous:

- **Unknown keys.** Read through the TOML decoder's `Undecoded` list. The
  common failure this catches is a typo: `workdri = "/src"` would otherwise
  vanish and the task would run in the image's default directory.
- **An unparsable manifest is `UNSUPPORTED`, not a load error.** `Discover`
  reports load errors only when *no* task in the set loads, so an erroring
  task in an otherwise healthy set would silently disappear from the report.
  An `UNSUPPORTED` task stays visible.
- **`format_version = 1` is required.** A manifest written for a later version
  may mean something this build cannot see.
- **`solution.kind = "none"` must be written out.** No reference solution is
  legitimate (D3), but an omitted `[solution]` table is likelier a mistake than
  a decision.
- **Fields that do not fit the chosen kind are refused**, e.g. `paths` with
  `exit_code`, or `env` on a patch. Accepting and ignoring them would let an
  author believe a setting is in force when it is not.
- **Paths must stay inside the task directory.** A task should move as one
  directory, and the leak check reasons about what is inside the build
  context; a path that climbs out breaks both.

### Departures from the roadmap sketch

- **`tests.mount` is required whenever `tests.dir` is set.** The sketch had
  `command = "./tests/run.sh"` beside `workdir = "/app"` and `dir = "tests"`,
  which only works if the tests land at `/app/tests`, and nothing said they
  would. The command is the author's, so where the tests are copied is
  something it depends on and Skeptic cannot choose.
- **`environment.context` is required with a Dockerfile.** "The task
  directory" is the obvious default, but whether `solution/` and `tests/` sit
  inside the context is exactly what the leak check reads. It has to be what
  the author meant.
- **Reward paths must end in `.json` or `.txt`.** The runner picks the parser by
  extension and reads anything that is not `.json` as a bare float, so
  `/logs/score.yaml` would be read one way and meant another.

The solution script's directory is uploaded to a fixed `/solution`. Unlike the
test location this is not configurable, because Skeptic builds the command that
runs the script, so the path never appears in anything the author writes.

The adapter is registered first. A directory holding both `skeptic.toml` and a
Harbor `task.toml` is claimed by the explicit manifest.

---

## D16. A reference solution that failed for want of network is ERROR

**Status:** decided, after a false flag.

On a host with restricted egress, `psf__requests-1921` scored `ORACLE_FAILS`.
Its graded tests make live HTTPS calls, and they failed because a proxy
intercepted TLS. The gold patch was never at fault; with working network the
instance is `CLEAN` (`results/swe-bench-verified/2026-09-26-native-repro`).
Skeptic had flagged a benchmark for the host's firewall, which is exactly what
the first rule in `CONTRIBUTING.md` forbids.

### The rule

When the oracle scores below 1.0 **and** its test output carries a
network-failure signature, the verdict is `ERROR`, with the score and the
matching line as the reason. Everything else is unchanged:

- An oracle at 1.0 is never questioned: the rule can only withdraw a bad score,
  never invent one.
- A nop that scores above 0 stands as `NOP_PASSES` even when the oracle hit
  the network. A missing network does not make a test pass.
- The worst the rule can do is turn a flag into `ERROR`, "could not check this
  task here". It never produces `CLEAN`.

### The signatures, and what is left out

Only messages the runtime writes and a test author would not:
`CERTIFICATE_VERIFY_FAILED`, the glibc, BSD and Node resolver errors,
`Network is unreachable`, `No route to host`, curl's `Could not resolve host`,
`requests.exceptions.ProxyError`. Matching is case-sensitive, so a test named
`test_certificate_verify_failed` does not match.

`Connection refused` and `timed out` are deliberately excluded. Test suites
start local servers and exercise timeouts on purpose, and matching either
would turn genuine failures into errors. `TestGenuineOracleFailureStaysFlagged`
names that case, per D12's rule that every filter must name the signal it may
not swallow.

### The cost

A genuinely broken reference solution, checked on a host whose network is
also broken, is reported `ERROR` rather than `ORACLE_FAILS`. That is the
honest answer: on that host the tool cannot tell the two apart. The signature
can also come from a harness's setup step (pip failing to upgrade itself)
rather than from a test. The reason line quotes the last match, not the first,
so it normally points at the test that failed.

### An approach that does not work

The roadmap first suggested re-running the oracle with `--network none` and
comparing. That cannot separate the cases. On a host whose network is already
broken, both runs fail the same way whether or not the solution is broken. The
comparison only has power where the network works, and there the oracle
passes and no question arises.

---

## D17. Resource limits are hard limits, and a memory kill is ERROR

**Status:** decided.

### What upstream does

Read from the Harbor source at `d10ac31`:

- `models/task/config.py` declares `cpus` and `memory_mb` as integers. It
  still accepts the deprecated `memory = "2G"`, migrates it with
  `_parse_size_to_mb`, and refuses to load a task whose `memory` and
  `memory_mb` disagree.
- `environments/base.py` resolves each resource through an enforcement policy
  that defaults to `auto`. The Docker environment maps `auto` to `limit`
  (`environments/docker/docker.py`), so by default both values become
  compose's `cpus` and `mem_limit`, which are hard limits.
- `--override-cpus` and `--override-memory-mb` replace the task's values.

Terminal-Bench 1.x has no such fields in `task.yaml`. Two of its 241 tasks,
`hf-lora-adapter` and `hf-train-lora-adapter`, set
`deploy.resources.limits.memory` in their compose file. The harness runs
`docker compose up`, and Compose applies those limits outside Swarm.
Reservations are not limits and are ignored.

Skeptic now applies all of these as `docker run --cpus/--memory`, mirrors the
size parsing exactly (so a truncation or refusal upstream is one here), and
adds the two override flags.

### A memory kill is ERROR

The roadmap's trap. A test process the kernel kills for memory did not fail;
it was stopped. Scoring what is left reads a limit as a verdict: an oracle at
0.00 would be `ORACLE_FAILS` against a working solution, and a nop at 0.00
would look like a healthy control without ever having run.

After the tests run, Skeptic asks Docker whether the container's OOM flag is
set, and if so the control is `ERROR`, naming the limit. Two things were
checked on a live daemon rather than assumed:

- **The flag is set for an exec'd process.** The controls exec into a
  container that only runs `sleep`, so the process killed is never the
  container's own. Docker sets `.State.OOMKilled` anyway, and the container
  keeps running. It is set by the time the exec returns (5 of 5 trials), and
  stays false when nothing was killed.
- **It works under cgroup v1.** The first idea was to read `oom_kill` from
  `/sys/fs/cgroup/memory.events` inside the container. That file only exists
  under cgroup v2, and the verification host was v1. The Docker flag reads
  the same on both.

Exit status 137 alone is not used: a timeout's kill produces it too, and a
test runner can survive the death of one of its workers and exit 1.

The flag stays set for the container's lifetime, so one check after the tests
also covers a solution script killed earlier. If the check cannot be made at
all, the control is `ERROR`: without the answer, a zero cannot be told from a
kill.

### What stays unlimited

A task declaring nothing runs without limits, as before. `storage_mb` and
GPUs are not applied; Docker cannot enforce the first portably, and Skeptic
has no GPU path.

---

## D18. A task that disagrees with itself is FLAKY

**Status:** decided.

`--repeat N` runs the nop and oracle controls N times each, every run in a
fresh container. Roadmap issue 2 asked for a deliberate answer to one
question: what is the verdict of a task whose oracle scores 1.0, 1.0, 0.0?

### A verdict of its own, and it fails CI

It is not `CLEAN`: one run in three says the reference solution fails. It is
not `ORACLE_FAILS` either: two runs say it passes. Any single score from such a
task is a sample, and reporting one as the result is the confident wrong
answer this tool exists to prevent. So `FLAKY` is its own verdict, and the
reason lists every score of both controls.

It is flagged. A task whose grade changes with nothing else changed cannot
rank agents, which is the same judgement SWE-bench makes when its own
pipeline runs each test three times and discards the inconsistent ones.

The fixture that verified this made the point unprompted. On its first real
run, the coin-flip task's *first* oracle run scored 0.00. Without `--repeat`,
it would have been reported as `ORACLE_FAILS`.

### Precedence

1. **Any run that cannot be read makes the task `ERROR`.** Each run is
   classified on its own first, so an error, a memory kill (D17) or a starved
   network (D16) in any one run wins. A score that disagrees because the host
   failed is the host's finding, and calling it `FLAKY` would blame the
   benchmark for this machine.
2. **Then any disagreement is `FLAKY`,** even when the runs also agree on
   something damning, such as a nop that passes every time. The grader has
   shown itself unreliable, so its consistent-looking outputs are no firmer
   than its inconsistent ones. The reason line still carries every score, so
   nothing is hidden.
3. **Otherwise the runs agree,** and the task is classified exactly as a
   single run would be.

Scores are compared exactly. They are ratios of test counts, so 0.98 against
1.00 is one test that passed once and failed once. That is the flake.

### Partial probes and the schema

The partial control runs once, and only when every oracle run scored 1.0.
Otherwise a probe's drop could be the flake, not the withheld hunk.

The report schema goes to 2. The verdict enum is closed, so a consumer
validating against schema 1 would reject `FLAKY`. Schema 2 also adds
`nop_scores` and `oracle_scores`, present only when there was more than one
run. Schema 1 reports are a subset and still load, since everything under
`results/` is one.

---

## D19. `check` had never scored a Terminal-Bench 1.x task

**Status:** fixed, after a validation run found it. Corrects part of D17.

### What was wrong

Asked to validate everything with parallel Compose runs, the Terminal-Bench
1.x fixture `testdata/tbench-tasks/clean` came back `ORACLE_FAILS`. The
evidence said `./run-tests.sh: not found`. The build before this session
(`38634de`) gives the same result, so this was never a regression. `check`
had simply never produced a correct score for this format:

- **The grading script was never delivered.** The adapter ran
  `./run-tests.sh` from `/`. Upstream images do not contain the script; the
  harness copies it in. `terminal_bench/harness/harness.py` `_setup_test_env`
  copies `run-tests.sh` and the *contents* of `tests/` into `/tests`
  (`_create_tar_archive` flattens the directory). `_run_tests` then runs
  `bash /tests/run-tests.sh`.
- **Both scripts ran from the wrong directory.** Upstream sends them to a
  tmux session in the container, whose directory is the image's `WORKDIR`.
  The adapter used `/`. The oracle agent (`terminal_bench/agents/oracle_agent.py`)
  copies `solution.sh` to `/oracle/solution.sh` and runs it with bash from
  there, so a solution using relative paths also broke.

Every reference solution therefore scored 0. The unit tests passed
throughout, because they asserted the fields the adapter set rather than
what upstream does. No committed result was affected: every Terminal-Bench
result under `results/` comes from `lint`, which runs nothing.

The fixtures hid it twice over. Their image lacked pytest, so even a
delivered script would have failed, and `no-solution`'s test checked
`echo hello`, which passes on an untouched workspace, so a working run would
have reported `NOP_PASSES` rather than the `NO_ORACLE` the fixture exists
for. Both fixtures now install pytest and grade a file the agent must write.
`e2e/tbench_test.go` runs them, and it is the test that would have caught
this.

### What Compose does with limits, measured

The same run checked D17's claim that Compose applies
`deploy.resources.limits` outside Swarm. Four compose files, brought up with
`docker compose` (v5.1.1) in parallel and inspected:

| Declared | Compose `HostConfig` | Skeptic `HostConfig` |
|---|---|---|
| `deploy` `memory: 4.0G`, reservation `2.0G` | Memory 4 GiB, **MemoryReservation 2 GiB** | Memory 4 GiB |
| `mem_limit: 512m`, `cpus: 0.5` | 512 MiB, 0.5 CPU | 512 MiB, 0.5 CPU |
| `deploy` `memory: 1gb`, `cpus: '1.5'` | 1 GiB, 1.5 CPU | 1 GiB, 1.5 CPU |
| `deploy` `memory: 3.5G` | 3.5 GiB | 3.5 GiB |

The limits match exactly. D17 was wrong in one detail. It said reservations
"are not limits and are ignored" by Docker. Compose does apply them, as
`MemoryReservation`, a soft limit the kernel enforces only when the host is
short of memory. Skeptic does not apply it. On a host with memory to spare
the two behave the same; under contention upstream would reclaim memory from
the container sooner. That is recorded here rather than silently copied,
because nothing yet shows it changes a verdict.

---

## D20. What `skeptic diff` calls a regression

**Status:** decided.

A **regression** is a task that gained a failure mode between two runs. Each
verdict names its modes: `NOP_PASSES` {nop}, `ORACLE_FAILS` {oracle},
`BOTH` {nop, oracle}, `FLAKY` {flaky}; `CLEAN` and `NO_ORACLE` have none. A
task regressed if the new run has a mode the old one did not. That includes a
swap such as `NOP_PASSES` → `ORACLE_FAILS`, which loses one mode and gains
another. It is **fixed** if it only lost modes. `CLEAN` ↔ `NO_ORACLE` is
**changed**: nothing was gained or lost, but whether the oracle could run did
change. The exit status is 1 only for a regression.

**`ERROR` and `UNSUPPORTED` are never compared.** Any move to or from either
goes under "could not compare", in both directions. This is the trap the
roadmap named, and it has bitten this project once already: the 2026-09-24
run's eight `ERROR`s were a full disk, and 2026-09-25's five were a Docker Hub
rate limit. A diff that reported `CLEAN` → `ERROR` as a regression would blame
the benchmark for the machine. The reverse, `ERROR` → `ORACLE_FAILS`, is not
one either. There is no earlier verdict to regress from, only an earlier run
that could not look.

A run where every task errored therefore exits 0 with everything under "could
not compare". That is deliberate, and the summary line makes it impossible to
miss. A CI job that wants a machine failure to fail the build should also check
`totals.errors` in the new report, which is a different question from "did
the benchmark change".

Verdicts are compared, not scores. An `ORACLE_FAILS` task whose oracle moved
from 0.50 to 0.20 is unchanged here. Nothing in the report yet says whether
such a move is the benchmark or the run.

Writing this found a bug in `report` itself. Merging a directory of fragments
stamped the result with the reading machine's host and the current time.
Fragments from an arm64 Mac merged on an amd64 Linux host therefore claimed
amd64, in a project where emulation against native was a real question. The
merge now takes both from the fragments.
