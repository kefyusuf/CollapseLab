# CF-001 Coordinated Omission — Implementation Plan and Execution Ledger

**Status:** Tasks 1–4 complete; Task 5 is the next implementation gate.  
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
- 1-second Prometheus scrape/evaluation interval,
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

## Task 5 — Evidence Parsing and Validity Model — NEXT

Implement CF-001-specific Go types/parsers for:

- k6 summary metrics,
- Prometheus query results,
- trigger window,
- rate stability,
- dropped iterations,
- achieved open arrival ratio,
- p99 and peak-inflight comparison,
- recovery window.

Result order:

```text
configuration
  -> measurement validity
  -> hypothesis assertions
```

An invalid measurement cannot become a hypothesis failure.

## Task 6 — CF-001 Runner and Evidence Bundle — PENDING

Runner responsibilities:

- load canonical config,
- record revision/environment/tool identity,
- start clean trial state,
- run closed trial,
- schedule deterministic trigger through control listener,
- capture actual trigger timestamps,
- collect k6 and Prometheus evidence,
- reset/verify recovery,
- run open trial,
- evaluate validity before hypothesis,
- write the evidence bundle.

No generic runner abstraction yet.

## Task 7 — Canonical Repetition Gate — PENDING

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

At Task 3 completion it is isolated from `main`; `main` remains bootstrap-only.

The next safe implementation boundary is **Task 5 only**.
