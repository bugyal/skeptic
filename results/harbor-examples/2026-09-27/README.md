# Harbor examples — before and after Compose support, 2026-09-27

All 38 tasks under Harbor's `examples/` (upstream `d10ac31`), run with the build before
D21 and the build after it, on the same host, then compared:

```
$ skeptic diff before.json after.json
Regressed — gained a failure mode
  harbor/environment-env-single  NO_ORACLE → NOP_PASSES  tests pass without any change (nop 1.00)

Could not compare — one side is ERROR or UNSUPPORTED; usually the machine, not the benchmark
  daytona-computer-use          ERROR → UNSUPPORTED  task needs host environment variable DAYTONA_API_KEY (for DAYTONA_API_KEY), which is not set
  harbor/environment-env-multi  UNSUPPORTED → NOP_PASSES  tests pass without any change (nop 1.00)
  harbor/hello-mcp              UNSUPPORTED → ERROR  build failed: compose build failed (exit 1)
  harbor/llm-judge-example      ERROR → UNSUPPORTED  task needs host environment variable ANTHROPIC_API_KEY (for ANTHROPIC_API_KEY), which is not set
  harbor/sidecar-artifacts      UNSUPPORTED → ERROR  build failed: compose build failed (exit 1)

1 regressed · 0 fixed · 0 changed · 5 could not compare · 0 added · 0 removed · 32 unchanged
```

**32 of 38 unchanged.** Changing the default working directory from `/app` to
the image's own altered no verdict here.

**The one "regression" is a fix.** `environment-env-single` tests that
`[environment.env]` reaches the image's entrypoint. Before D21 Skeptic cleared
the entrypoint, so the test failed and the blank answer scored 0.00 for the
wrong reason. Now the entrypoint runs, the test passes without any agent work,
and `NOP_PASSES` is the correct verdict for a task that checks the harness
rather than an agent. `diff` reports it as a gained failure mode, which it is.

**Could not compare, and why:**

- `environment-env-multi`: was `UNSUPPORTED` (two services), now runs and is
  `NOP_PASSES` for the same reason as its single-container twin.
- `hello-mcp`, `sidecar-artifacts`: now attempted as Compose stacks; their
  images `apt-get install` at build time, which this host's network blocks.
  `ERROR` is the machine.
- `daytona-computer-use`, `llm-judge-example`: were `ERROR` (they were run
  with the literal text `${DAYTONA_API_KEY}` and `${ANTHROPIC_API_KEY}` as
  their keys); now `UNSUPPORTED`, naming the host variable each needs.

Nothing was left behind: no Compose projects, containers or volumes.

| File | Contents |
|---|---|
| `before.json`, `after.json` | Schema-2 reports from the two builds |
