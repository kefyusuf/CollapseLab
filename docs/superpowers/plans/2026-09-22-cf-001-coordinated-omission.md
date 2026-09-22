# CF-001 Coordinated Omission — Implementation Plan and Execution Ledger

**Status:** Tasks 1–3 complete; Task 4 is the next implementation gate.  
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

## Task 4 — Explicit k6 Workload Semantics — NEXT

Goal: implement two inspectable scripts using the same k6 image.

### Closed workload

Must use:

```text
executor = constant-vus
vus      = 5
duration = 30s
```

Requirements:

- target only `http://sut-lab:8080/work`,
- send `X-CollapseLab-Scenario: closed`,
- no artificial sleep that changes closed-loop semantics,
- collect a dedicated `work_latency` metric for `/work`,
- emit machine-readable summary JSON.

### Open workload

Must use:

```text
executor          = constant-arrival-rate
rate              = 100/s
preAllocatedVUs   = 100
duration          = 30s
```

Requirements:

- target only `http://sut-lab:8080/work`,
- send `X-CollapseLab-Scenario: open`,
- maintain arrival scheduling independently of response completion,
- expose `dropped_iterations`,
- emit the same dedicated `work_latency` metric and summary shape.

### Task 4 acceptance

Before Task 5:

- scripts pass k6 syntax/runtime checks using the pinned image,
- the executor types are mechanically asserted,
- target host is mechanically asserted as `sut-lab`,
- scenario labels are exact,
- no sleep-based pacing appears in the closed script,
- open run surfaces dropped-iteration evidence,
- generated summary files are parseable JSON.

Do not yet decide whether the experiment hypothesis passes; Task 4 establishes workload semantics only.

## Task 5 — Evidence Parsing and Validity Model — PENDING

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

The next safe implementation boundary is **Task 4 only**.
