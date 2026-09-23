# CF-001 Coordinated Omission — Implementation Plan and Execution Ledger

**Status:** Tasks 1–6 complete; Task 7 is the next implementation gate.  
**Spec:** `docs/superpowers/specs/2026-09-22-collapselab-v0.1-design.md`

## Goal

Build the smallest end-to-end CollapseLab experiment that proves a closed workload can hide a deterministic service stall while an open arrival workload exposes the tail, while distinguishing valid evidence from measurement failure.

## Locked Stack

- Go 1.27.1
- Docker Compose v2
- Grafana k6 2.2.0
- Prometheus 3.14.0
- `github.com/prometheus/client_golang` 1.24.1
- `go.yaml.in/yaml/v3` 3.0.5

## Global Constraints

- Docker Compose first and local-only.
- No Kubernetes, Kafka, service mesh, remote workers, browser UI, or AI diagnosis.
- Same k6 binary for closed and open workload modes.
- Measurement invalidity is evaluated before hypothesis assertions.
- Load generator saturation invalidates the open-model run.
- Control endpoints remain loopback/local only.
- Do not extract a generic multi-experiment framework during CF-001.
- Every performance claim must be revision/configuration/tool-version bound.

## Canonical Experiment Profile

```yaml
schema_version: 1
id: CF-001
name: coordinated-omission
version: 1
sut:
  base_service_time: 50ms
  work_path: /work
trial:
  duration: 30s
  trigger_after: 10s
  stall_duration: 500ms
closed:
  executor: constant-vus
  vus: 5
open:
  executor: constant-arrival-rate
  rate_rps: 100
  preallocated_vus: 100
validity:
  pretrigger_rate_tolerance_ratio: 0.10
  max_dropped_iterations: 0
  minimum_open_achieved_ratio: 0.99
  maximum_pretrigger_rate_cv: 0.15
hypothesis:
  minimum_p99_ratio_open_over_closed: 5.0
  minimum_peak_inflight_ratio_open_over_closed: 5.0
  maximum_closed_p99_ms: 150
  minimum_open_p99_ms: 250
recovery:
  stability_window: 5s
  maximum_posttrigger_rate_cv: 0.15
```

## Task 1 — Configuration Contract — COMPLETE

Produces:

- strict typed experiment configuration,
- human-readable duration decoding,
- canonical CF-001 YAML,
- validation of workload model and evidence thresholds.

Important invariant:

```text
minimum open VUs =
ceil(rate_rps × stall_duration_seconds) + 10
```

Evidence:

- unknown YAML fields rejected,
- invalid durations rejected,
- invalid executor/profile bounds rejected,
- canonical config loads exactly,
- exact Go 1.27.1 integration subsequently verified by CI.

## Task 2 — Deterministic SUT — COMPLETE

Produces:

- Go HTTP SUT,
- public listener `:8080`,
- control listener `:9091`,
- deterministic global stall gate,
- exact planned/actual trigger timestamps,
- overlap rejection,
- context-aware waiting,
- bounded scenario labels: `closed|open|unknown`,
- Prometheus request, duration, in-flight, stall-count, and stall-active metrics,
- graceful bounded shutdown.

Critical measurement regression covered:

> request duration is observed at request completion, not at defer-registration time.

Race suite is green under exact Go 1.27.1 CI.

## Task 3 — Local Infrastructure — COMPLETE

Produces:

- pinned Go multi-stage/Scratch SUT image,
- Prometheus 3.14.0,
- k6 2.2.0 tool profile,
- internal experiment network,
- separate host-access management bridge,
- Prometheus evidence collection; CF-001 scrape resolution is 100ms with 1s rule evaluation after Task 5 measurement-integrity hardening,
- loopback-only host ports,
- bounded readiness polling,
- deterministic teardown,
- CI runtime diagnostics.

### Runtime RED→GREEN history

1. **Run #1:** failed because `go.sum` lacked Prometheus transitive checksums.
2. **Run #2:** Go 1.27.1 race suite and Compose parse passed; SUT readiness failed.
3. **Run #3:** diagnostics proved both containers were running, while all host probes were refused and no published ports appeared.
4. **Fix:** keep `lab` internal, add `host_access` bridge, pin internal SUT identity to `sut-lab`.
5. **Run #4:** smoke passed; a one-shot Prometheus target assertion raced first discovery.
6. **Fix:** bounded condition-based target polling; move diagnostics after all runtime assertions.
7. **Run #5:** entire infrastructure gate passed.

Canonical Task 3 evidence:

- Actions run: `35682953253`
- Verified head: `80a9d1bd809bb84b0afd59c4c6e36911df737c0c`

## Task 4 — Explicit k6 Workload Semantics — COMPLETE

Produces two inspectable workloads using the same pinned `grafana/k6:2.2.0` image.

### Closed workload

```text
executor = constant-vus
vus      = 5
duration = 30s
```

Properties:

- target is exactly `http://sut-lab:8080/work`,
- header is exactly `X-CollapseLab-Scenario: closed`,
- no sleep-based pacing,
- dedicated `work_latency` Trend records only `/work` request duration,
- machine-readable summary is written under `runs/cf001/`.

### Open workload

```text
executor          = constant-arrival-rate
rate              = 100/s
preAllocatedVUs   = 100
maxVUs            = 100
duration          = 30s
```

Properties:

- target is exactly `http://sut-lab:8080/work`,
- header is exactly `X-CollapseLab-Scenario: open`,
- arrival scheduling is independent from request completion,
- dynamic VU expansion is deliberately disabled by setting `maxVUs == preAllocatedVUs`,
- generator saturation therefore remains visible as `dropped_iterations` instead of being hidden by worker-pool growth,
- machine-readable summary normalizes dropped-iteration evidence as both `present` and numeric `count`,
- the same `work_latency` summary shape is used as the closed workload.

### TDD evidence

RED:

- Actions run `#7` / ID `35698257737`
- head `a10b5cf14f5f9ad4681e13d14f0fc268a0f7d843`
- failure was exactly the missing `closed.js`, `open.js`, and `summary.js` contract files.

GREEN:

- Actions run `#8` / ID `35698455401`
- head `a9a39ead1c890c6c3cc6e508cfb66e52fc3fbfc5`
- Go 1.27.1 race suite: PASS
- pinned k6 inspect: PASS
- closed canonical run: PASS
- open canonical run: PASS
- both summary JSON documents parse and satisfy the shared schema: PASS
- teardown: PASS

Execution evidence from run #8:

```text
closed:
  5 looping VUs
  30s
  2950 completed iterations
  0 interrupted iterations

open:
  100.00 iterations/s
  maxVUs = 100
  30s
  3001 completed iterations
  0 interrupted iterations
```

These numbers are workload-semantics evidence only. They are not yet CF-001 hypothesis results because no deterministic stall was orchestrated in Task 4.

### Task 4 ruling

`maxVUs` is fixed to `100`, equal to `preAllocatedVUs`.

Reason: CF-001 must detect generator saturation. Allowing k6 to grow the worker pool dynamically could mask insufficient preallocation and contaminate measurement validity.

Cost if wrong: a future legitimate workload requiring more than 100 VUs will surface dropped iterations and become INVALID rather than silently scaling the generator. This is the safer failure mode for CF-001.

## Task 5 — Evidence Parsing and Validity Model — COMPLETE

Task 5 establishes the boundary between **measurement validity** and **hypothesis evaluation**.

The evaluation order is locked:

```text
configuration
  -> evidence parsing
  -> measurement validity
  -> VALID?
       |-- no  -> INVALID
       '-- yes -> hypothesis assertions
                    |-- pass -> SUPPORTED
                    '-- fail -> NOT_SUPPORTED
```

An invalid measurement is never reported as a failed systems hypothesis.

### Evidence parsers

CF-001 now has typed parsers for:

- k6 summary evidence,
- Prometheus matrix query responses,
- completed SUT trigger windows.

The k6 parser requires:

- schema version 1,
- scenario identity `closed|open`,
- `work_latency p(99)`,
- iteration rate,
- normalized dropped-iteration evidence,
- `http_req_failed.rate`,
- failed check count.

The Prometheus matrix parser preserves labels and timestamped samples and rejects:

- non-success API responses,
- non-matrix responses,
- malformed samples,
- non-finite values,
- non-monotonic sample timestamps.

The trigger parser requires:

- positive generation,
- actual start,
- actual end,
- inactive/completed state,
- end strictly after start.

### Measurement validity

A pair is `INVALID` before hypothesis evaluation when any required measurement invariant fails.

Current validity gates include:

- invalid experiment configuration,
- scenario identity mismatch,
- incomplete trigger window,
- non-finite/invalid evidence,
- open achieved arrival ratio below the configured minimum,
- dropped iterations above the configured maximum,
- any HTTP request failure,
- any failed exact-204 check,
- unstable pre-trigger rate,
- non-comparable closed/open pre-trigger rates,
- material telemetry gaps,
- insufficient in-flight telemetry across the actual stall window,
- insufficient post-trigger telemetry to evaluate recovery.

When `INVALID` is returned, `HypothesisReasons` must remain empty.

### Hypothesis evaluation

Only valid evidence reaches hypothesis assertions.

CF-001 currently evaluates:

- closed p99 ceiling,
- open p99 floor,
- open/closed p99 ratio,
- open/closed peak in-flight ratio.

If valid evidence misses one or more hypothesis thresholds, status is `NOT_SUPPORTED`.

If all hypothesis thresholds pass, status is `SUPPORTED`.

### Recovery semantics

Recovery is evaluated independently from measurement validity.

- **Enough post-trigger telemetry + no stable recovery window** = valid observed non-recovery.
- **Not enough telemetry to decide whether recovery occurred** = `INVALID`.

This prevents a real recovery failure from being mislabeled as a broken measurement while still rejecting evidence that cannot support a recovery conclusion.

### Stall-scale observability correction

Task 5 self-review found a measurement flaw in the original infrastructure:

```text
canonical stall = 500ms
Prometheus scrape = 1s
```

A 1-second scrape cannot reliably capture peak in-flight behavior during a 500ms event.

A regression test now requires:

```text
scrape_interval <= stall_duration / 2
```

Canonical Prometheus settings are now:

```yaml
scrape_interval: 100ms
scrape_timeout: 90ms
evaluation_interval: 1s
```

This gives multiple scrape opportunities inside the canonical 500ms stall.

### Real-runtime parser verification

Synthetic fixtures are not the only parser evidence.

CI runs the pinned `grafana/k6:2.2.0` closed and open workloads, writes their real summary files, then executes the Go parser against those exact generated files.

The runtime summary gate also requires:

- `p(99)` in `work_latency`,
- `http_req_failed.rate == 0`,
- `checks.fails == 0`,
- parseable closed/open scenario identity.

### TDD / verification history

Core parser/evaluator gate:

- RED run #11 / `35723020997`
  - expected missing parser/evaluator APIs.
- GREEN run #12 / `35723333278`
  - parser/evaluator tests,
  - Go race suite,
  - pinned k6 workloads,
  - infrastructure gate all passed.

Observability-resolution gate:

- RED run #13 / `35723612560`
  - exact failure: `scrape interval 1s cannot reliably observe 500ms stall; want <= 250ms`.
- first runtime integration exposed a repository-relative evidence-path bug.
- GREEN run #16 / `35724005834`
  - 100ms Prometheus scrape accepted at runtime,
  - real closed/open summaries successfully parsed by `ParseK6Summary`.

Request-success validity gate:

- RED run #17 / `35724363406`
  - expected missing `HTTPReqFailedRate` and `ChecksFailed` evidence fields.
- final GREEN run #18 / `35724503011`
  - full Go race suite: PASS,
  - 100ms Prometheus runtime: PASS,
  - pinned k6 inspect: PASS,
  - closed/open workloads: PASS,
  - actual generated summaries: PASS,
  - HTTP/check validity evidence: PASS,
  - teardown: PASS.

Final Task 5 production head:

`f938727465aa88e82dce1f2db332b3317b8b3e94`

## Task 6 — CF-001 Runner and Revision-Bound Evidence Bundle — COMPLETE

Task 6 composes the previously verified components into one real, revision-bound CF-001 execution.

The runner deliberately remains CF-001-specific. No generic multi-experiment runtime has been extracted yet.

### Canonical execution flow

```text
canonical experiment.yaml
        |
        v
strict config validation
        |
        v
Git revision + cleanliness gate
        |
        v
create immutable run directory
        |
        v
clean Docker lab startup
        |
        +--> closed workload
        |      -> traffic observed
        |      -> deterministic stall scheduled
        |      -> actual stall timestamps captured
        |      -> k6 summary
        |      -> Prometheus rate/in-flight evidence
        |
        +--> open workload
               -> traffic observed
               -> deterministic stall scheduled
               -> actual stall timestamps captured
               -> k6 summary
               -> Prometheus rate/in-flight evidence
        |
        v
Task 5 validity/evaluation model
        |
        v
assertions + timeline + report
        |
        v
manifest + SHA-256 artifact binding
        |
        v
VerifyRunBundle
```

### Revision safety

Canonical evidence is refused when the repository is dirty.

The Git revision source records:

- exact commit SHA,
- branch,
- repository remote,
- dirty state.

Cleanliness includes both tracked changes and **untracked non-ignored files**.

Generated evidence under the gitignored `runs/` tree does not dirty the revision.

TDD evidence:

- RED run #28 / `35783501368`
  - an untracked source file was incorrectly ignored by the prior `--untracked-files=no` status check.
- GREEN run #29 / `35813526816`
  - `git status --porcelain --untracked-files=normal` correctly rejects untracked source while honoring `.gitignore`.

This prevents a bundle from claiming to describe commit X while local, uncommitted source files actually influenced the run.

### Runtime/environment identity

`environment.json` records the runtime identity used for the experiment, including:

- Go runtime version,
- Docker server version,
- Docker Compose version,
- GOOS / GOARCH,
- CPU count,
- pinned k6 image plus resolved image ID,
- pinned Prometheus image plus resolved image ID,
- built SUT image ID.

Task 6 does not infer identity from mutable image tags alone.

### Immutable bundle contract

Canonical bundle shape:

```text
runs/cf-001/<run-id>/
├── manifest.json
├── environment.json
├── experiment.yaml
├── revision.json
├── timeline.jsonl
├── closed/
│   └── k6-summary.json
├── open/
│   └── k6-summary.json
├── metrics/
│   ├── closed.json
│   └── open.json
├── assertions.json
└── report.md
```

The manifest binds:

- run ID,
- experiment ID/version,
- creation timestamp,
- exact revision SHA,
- exact config SHA-256,
- SHA-256 and byte size for every persisted evidence artifact.

After finalization the `RunBundle` API refuses further writes.

`VerifyRunBundle` independently verifies:

- run-directory identity,
- experiment config digest,
- revision/manifest SHA agreement,
- `dirty:false`,
- every declared artifact digest and size,
- required bundle metadata.

Incomplete runner failures remove their partial bundle rather than leaving misleading canonical evidence behind.

### Orchestration/runtime verification history

Task 6 implementation progressed through explicit RED/GREEN gates:

- run #20 / `35781068863` — RED: revision-bound bundle contract.
- run #21 / `35781197249` — GREEN: immutable manifest/bundle primitives.
- run #22 / `35781548190` — RED: runner orchestration contract.
- run #23 / `35781617412` — RED while correcting Prometheus fixture encoding.
- run #24 / `35781771981` — GREEN: runner core + evidence persistence.
- run #25 / `35782059511` — RED: real revision-bound runner bundle required.
- run #26 / `35782563298` — real canonical execution exposed an image-identity templating failure.
- run #27 / `35782924708` — GREEN: first complete real runner execution and uploaded bundle.
- run #28 / `35783501368` — RED: untracked source was missing from dirty-revision detection.
- run #29 / `35813526816` — GREEN: exact-head runner/bundle after revision-cleanliness fix.
- run #30 / `35813841412` — RED: threshold reason rounding hid a real decision boundary.
- run #31 / `35813897428` — GREEN: hypothesis reasons preserve six-decimal decision precision.
- run #32 / `35814162202` — RED: report incorrectly called `NOT_SUPPORTED` a measurement status.
- run #33 / `35814204850` — final GREEN: evaluation-label semantics, full runner, bundle verification, artifact upload, and teardown all pass.

### Final exact-head evidence

Verified implementation head:

`bcf6c34273c344fe3b9bc7f00b2517f9659154b8`

GitHub Actions:

- run #33
- run ID: `35814204850`
- conclusion: **SUCCESS**

Uploaded artifact:

- name: `cf001-task6-35814204850`
- artifact ID: `10730889183`
- archive digest: `sha256:16231215700b2e54e13e8e629d1cb02a7bbfc712587596c3925222e885058c9b`

Independent artifact audit:

- manifest revision SHA matches exact workflow head: PASS
- `revision.dirty == false`: PASS
- config SHA-256 binding: PASS
- 10 declared evidence artifacts: PASS
- every declared SHA-256 digest: PASS
- every declared byte size: PASS
- report status terminology: PASS
- precision of threshold reasons: PASS

### Latest canonical run result

The final Task 6 run produced **valid measurement evidence**, but the configured hypothesis is:

`NOT_SUPPORTED`

Observed values:

```text
closed p99                  51.556193 ms
open p99                   244.238201 ms
open / closed p99 ratio      4.737320
closed peak in-flight        5
open peak in-flight         32
peak in-flight ratio         6.4
open achieved-rate ratio     0.998602
closed recovered             true
open recovered               true
```

Hypothesis reasons:

```text
open p99 244.238201ms below 250.000000ms
p99 ratio 4.737320 below 5.000000
```

This is **not** an execution or measurement failure.

The measurement passed validity checks; the exact configured hypothesis thresholds were simply not met in this run.

The thresholds are intentionally **not** changed to manufacture a supported result. Task 7 owns repetition and reproducibility analysis across at least three clean canonical runs.

### Task 6 ruling

Task 6 is complete because its acceptance criterion is a trustworthy runner and revision-bound evidence bundle, not a predetermined hypothesis outcome.

A `SUPPORTED`, `NOT_SUPPORTED`, or `INVALID` experiment result must be persisted honestly as long as runner execution and evidence integrity are correct.

## Task 7 — Canonical Repetition Gate — NEXT

Run at least three clean canonical repetitions.

Require:

- all runs valid,
- same qualitative mechanism,
- no generator saturation,
- comparable pre-trigger load,
- stable recovery behavior,
- no material telemetry gaps around trigger.

Only curated summaries may enter Git.

## Task 8 — Documentation, Whole-Branch Verification, and Review — PENDING

Before merge:

- full Go race suite,
- Compose/runtime gate,
- canonical CF-001 run sequence,
- evidence-bundle integrity check,
- documentation of mechanism and limitations,
- whole-branch review,
- external review gate when available,
- no merge while Important/Critical findings remain.

## Current Branch State

Feature branch:

`feat/cf-001-coordinated-omission`

At Task 6 completion it remains isolated from `main`; `main` remains bootstrap-only.

The next safe implementation boundary is **Task 7 only**.
