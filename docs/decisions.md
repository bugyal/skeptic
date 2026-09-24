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
