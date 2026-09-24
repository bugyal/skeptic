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
