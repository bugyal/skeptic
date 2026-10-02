# Roadmap

What is planned, and where to start if you want to help.

The issues below are written to be picked up cold. Each names the files
involved, what "done" looks like, and the trap to avoid. They are ordered by how
useful they are, not by difficulty.

---

## 1. A custom `skeptic.toml` format — done

Built in `internal/adapter/custom/`, documented in the README under
"Custom `skeptic.toml`", with fixtures in `testdata/custom/` and an e2e test in
`e2e/custom_test.go`. Where it departs from the sketch that stood here, and
why, is in `docs/decisions.md` D15.

---

## 2. `--repeat N` for flake detection — done

`--repeat N` runs each control N times and reports disagreement as the new
`FLAKY` verdict. The report schema went to 2 for it. `docs/decisions.md` D18
says why a flaky task gets its own verdict and why a host failure in any run
is `ERROR` instead.

---

## 3. `skeptic diff` between two runs — done

`skeptic diff old new` sorts tasks into regressed, fixed, changed, could not
compare, added and removed, and exits 1 only on a regression. A move to or
from `ERROR` is never one. `docs/decisions.md` D20 defines a regression.

---

## 4. Honour per-task resource limits — done

Harbor's `cpus`, `memory_mb` and legacy `memory`, Terminal-Bench 1.x's compose
`deploy.resources.limits`, and two new `skeptic.toml` keys are applied as
hard limits, with `--override-cpus` and `--override-memory-mb` to replace
them. A test killed at the limit is `ERROR`. `docs/decisions.md` D17 has the
details, including what Harbor's source said that the sketch here did not.

---

## 5. A Homebrew tap — config ready, the rest is the owner's

The release config is done (`.goreleaser.yaml` `homebrew_casks`; why a cask
and what it does are in `docs/decisions.md` D22). It stays inert until these
steps, which only the repository owner can take:

1. Create a public repository `bugyal/homebrew-tap`. An initial README is
   fine; goreleaser writes `Casks/skeptic.rb` to its default branch.
2. Create a fine-grained personal access token limited to that one
   repository, with **Contents: read and write** and nothing else.
3. In `bugyal/skeptic` → Settings → Secrets and variables → Actions, add it
   as `HOMEBREW_TAP_GITHUB_TOKEN`.
4. Tag the next release as usual. Its workflow uploads the cask. v0.4.0 is
   the first release that carries the config; if its tag is pushed before
   the secret exists, re-run its release workflow afterwards. Releases before
   it never upload a cask.
5. On an Apple Silicon Mac and an Intel Mac: `brew install
   bugyal/tap/skeptic`, then `skeptic version` and `skeptic lint` on a task
   directory with Docker stopped. That last check is the roadmap's trap: the
   tap must not require Docker.

Decide first whether to keep the post-install hook that clears macOS's
quarantine flag. D22 explains the trade-off.

---

## 6. Do not flag a benchmark for the host's network — done

A failing oracle whose test output shows the host could not reach the network
is now `ERROR`, not `ORACLE_FAILS`. The approach first suggested here,
re-running with `--network none` and comparing, cannot tell the cases apart;
`docs/decisions.md` D16 says why and records what was built instead.

A related check is still unbuilt: a benchmark whose graded tests *need* the
internet is fragile in its own right and could be reported as such, as a
separate finding rather than folded into a verdict.

---

## Open, and blocked on access rather than work

**Two SWE-bench Verified instances have never been scored cleanly:**
`pylint-dev__pylint-6386` was never reached, and `astropy__astropy-13398` is
quarantined because its result came from a build with a known bug (D11).
Both need the augmented dataset, which only Hugging Face serves; the session
that did the 0.2.0 work could not reach it. On any machine that can:

```sh
python -c "from datasets import load_dataset; \
  load_dataset('SWE-bench/SWE-bench_Verified', split='test').to_json('verified.jsonl')"
skeptic check verified.jsonl --task pylint-dev__pylint-6386 --task astropy__astropy-13398 \
  --partial --json two.json
```

On arm64 each takes about four minutes under emulation, and both images are
about 4 GB. Record the result beside `results/swe-bench-verified/2026-09-25-batch60`.

---

## Larger, not yet specified

- ~~**Multi-service compose orchestration.**~~ Done: see `docs/decisions.md`
  D21. Still refused: a Terminal-Bench task with no service named as the
  client container, and Harbor's Windows tasks.
- ~~**Harbor multi-step tasks.**~~ Done: see `docs/decisions.md` D24.
  Still refused: multi-step tasks that also need a separate verifier
  container or a per-step network switch (D23).
- ~~**Exercise the remaining log parsers against real instances.**~~ Done:
  all twelve SWE-bench parsers ran against live instances on 2026-09-25
  (`results/swe-bench-verified/2026-09-25-controls`), and the same subset was
  reproduced natively and in parallel on 2026-09-26.
- **A full SWE-bench Verified run.** Needs roughly 2 TB of image traffic and a
  machine with real disk. The result belongs under `results/`.
- **Adapters for other task formats**, if and when ones with a reference
  solution and a machine-readable score appear. The adapter interface is three
  methods; the constraint is the format, not the plumbing.

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md). The one rule: Skeptic must never
report a verdict it did not earn. If the tool cannot check something faithfully,
`ERROR` and `UNSUPPORTED` are the honest answers.
