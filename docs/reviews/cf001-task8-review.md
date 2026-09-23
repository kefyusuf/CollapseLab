# CF-001 Task 8 — Whole-Branch Review

**Review scope:** `main@06e9c4fd5ee4bf530bd930f661a34c566da1e4f7` through CF-001 v0.1 feature implementation.  
**Implementation reviewed through:** `bc4f55b361281d80f382a07ba35880642c56c12a` before documentation closeout.  
**Review type:** author whole-branch correctness/security/reproducibility review plus CI evidence.

## External review status

The installed CodeRabbit review workflow requires the CodeRabbit CLI.

Task 8 attempted to use it, but:

```text
coderabbit: command not found
curl: (6) Could not resolve host: cli.coderabbit.ai
```

The execution environment could therefore not install or run CodeRabbit. **No CodeRabbit review result is claimed.**

This is weaker than a fresh independent reviewer. The feature branch is intentionally not merged by Task 8; the integration choice and any available human/external review remain a landing decision.

## Whole-branch review result

After the fixes below:

```text
open Critical findings:  0
open Important findings: 0
```

### Important — repetition verifier path containment

**Finding:** a manipulated repetition summary could provide a run ID such as `../outside-run`; verification joined that value to `runsRoot` before validating the ID and could read a bundle outside the intended repetition root.

**RED:** run #38 / `35915229009`  
**Fix:** validate `run_id` with the existing run-ID contract before child path construction.  
**GREEN:** run #39 / `35915314759`

### Important — configuration could claim values the runtime ignored

**Finding:** CF-001 YAML previously accepted changes to service time, work path, trial duration, VU count, arrival rate, and preallocation while Compose/k6 continued using hard-coded v0.1 values. Evidence could therefore be correctly hashed to a config that did not describe the actual execution.

**RED:** run #40 / `35916053458`  
**Fix:** explicitly bind CF-001 v1 validation to the implemented canonical workload shape; retain configurability only where the runtime actually consumes it and enforce control-plane bounds.  
**Intermediate:** run #41 caught an unused-import compile error during the fix.  
**GREEN:** run #42 / `35916236370`

### Important — duplicate repetitions and Git remote credential leakage

**Finding 1:** a repetition assessment could count the same `run_id` multiple times and still pass.  
**Finding 2:** `git remote get-url origin` was stored directly in `revision.json`; an HTTPS remote containing userinfo credentials could leak them into evidence and failure diagnostics.

**RED:** run #43 / `35916986102`  
**Fixes:**

- duplicate child run IDs now fail the repetition gate;
- URL userinfo is removed from Git remote evidence before persistence.

**GREEN:** run #44 / `35917120719`

## Prior review fixes already present

Task 8 also re-verified earlier evidence-integrity fixes from Tasks 5–7:

- dirty revision detection includes untracked non-ignored files;
- run bundles reject unsafe artifact paths and symlinks;
- generated artifact SHA-256/size is verified;
- revision/config/environment identity is checked across repetitions;
- threshold reasons preserve decision precision;
- `INVALID` measurement cannot be reported as hypothesis failure;
- `NOT_SUPPORTED` is labeled as an evaluation result, not measurement invalidity.

## Documentation review

Task 8 adds/updates:

- root `README.md`;
- `ARCHITECTURE.md`;
- `experiments/cf-001-coordinated-omission/README.md`;
- this review record.

Public docs now state the actual Task 7 result rather than presenting the configured 5× threshold as if it were achieved.

## CI review

Task 8 adds explicit:

```bash
go vet ./...
```

to the existing race/runtime/repetition pipeline.

`actions/upload-artifact` is moved from v4 to v6 so the workflow uses the Node 24 generation of the action instead of emitting the Node 20 deprecation warning observed during Task 7.

## Deferred Minor / operational items

These do not block the CF-001 v0.1 review:

1. **CI cost:** the three-run repetition gate currently runs on every matching feature-branch push and pull request. A future CI design may split fast PR checks from an explicit canonical evidence gate, but must preserve a required full run before landing.
2. **Trusted-container boundary:** k6 and the SUT share the internal lab network. The control listener is separated at the HTTP/listener level but Docker-local components are trusted; this is not a hostile multi-tenant sandbox.
3. **Platform support:** the canonical local runner uses Unix `id -u/-g`; Windows users should run it under WSL2.
4. **Image source pinning:** version tags are paired with resolved local image IDs in evidence, but source files do not yet pin registry digests.
5. **Telemetry claim:** evidence verifies the Prometheus query-result surface used by CF-001; it is not a cryptographically complete raw scrape archive.
6. **Licensing:** no repository license is selected in this task. A license choice should be explicit rather than inferred.

## Landing gate

Task 8 does not merge the branch.

Before integration, the exact documentation/CI closeout head must pass:

- `go test -race ./...`;
- `go vet ./...`;
- Compose/runtime smoke checks;
- pinned k6 checks;
- three canonical revision-bound CF-001 repetitions;
- repetition summary verification and enforcement;
- deterministic teardown.

Integration remains a human decision under the branch-finishing workflow.
