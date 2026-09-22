# CollapseLab v0.1 — Product, Scope, and Architecture Design

**Status:** Approved for CF-001 implementation  
**Date:** 2026-09-22  
**Repository:** `kefyusuf/CollapseLab`  
**Tagline:** Reproduce, observe, explain, and recover from production-scale failure loops.

## 1. Product Thesis

CollapseLab is a local-first production-systems laboratory for reproducing failure mechanisms that appear under load, contention, recovery, or interactions between otherwise healthy components.

It is deliberately **not**:

- a generic high-traffic tutorial,
- a framework benchmark leaderboard,
- a microservices showcase,
- a generic chaos-engineering platform,
- an SRE dashboard product,
- an AI root-cause-analysis product.

The unit of value is a **reproducible experiment with revision-bound evidence**.

A valid experiment tells the complete story:

```text
baseline
  -> trigger
  -> propagation
  -> amplification
  -> misleading signal
  -> naive fix (when useful)
  -> recovery
  -> mitigation
  -> evidence
```

The central quality rule is:

> No performance or resilience claim is valid without reproducible evidence bound to the exact experiment configuration and revision.

## 2. Target Audience

Primary:

- senior backend engineers,
- SRE/platform engineers,
- system designers,
- engineers preparing for staff/principal-level systems work,
- maintainers investigating non-obvious production failure modes.

## 3. Design Principles

### P1 — Mechanism first

Experiments are named after failure mechanisms, not technology brands.

Prefer:

- `coordinated-omission`
- `retry-metastability`
- `connection-budget-collapse`

Avoid:

- `redis-lab`
- `postgres-lab`
- `kafka-lab`

### P2 — Evidence before advice

Every meaningful claim records:

- workload definition,
- environment identity,
- exact revision,
- tool/image versions,
- timestamps,
- raw or machine-readable measurements,
- comparison criteria.

### P3 — Goodput over raw throughput

Where relevant, distinguish:

- offered load,
- admitted load,
- completed work,
- deadline-valid work,
- useful goodput,
- retry work,
- abandoned/expired work,
- wasted work.

### P4 — Recovery is first-class

A failure demonstration is incomplete unless it asks:

- Does the system self-recover?
- How long does recovery take?
- Can recovery traffic cause a second failure?
- What breaks the sustaining loop?
- Is the mitigation itself safe during recovery?

### P5 — Wrong fixes are educational assets

When deterministic and safe, experiments should demonstrate plausible mitigations that worsen the failure.

### P6 — Real infrastructure, minimal business logic

Use real infrastructure mechanics where they matter, but keep the SUT minimal enough that causality remains visible.

### P7 — Local-first and safe by default

v0.1 is local-only. Fault injection and load generation do not target arbitrary external hosts.

### P8 — Reproducibility over spectacle

Prefer a smaller deterministic experiment over a visually impressive but noisy system.

### P9 — Framework-neutral semantics

The first reference SUT is Go, but experiment semantics must not depend on Go, Laravel, Spring, Node.js, or another framework.

### P10 — Complexity must be earned

Kubernetes, Kafka, service mesh, browser UI, remote execution, and AI diagnosis remain out of v0.1 until an experiment proves the need.

## 4. v0.1 Flagship Experiments

v0.1 is organized around six mechanisms:

1. **CF-001 — Coordinated Omission**  
   A closed workload hides a deterministic service stall while an open arrival workload exposes the tail.

2. **CF-002 — Retry Metastability**  
   A temporary trigger ends, but retry amplification sustains degraded behavior.

3. **CF-003 — Scale-Out Connection Budget Collapse**  
   Additional application replicas multiply downstream connection demand and reduce availability.

4. **CF-004 — Low-CPU Lock Collapse**  
   The service becomes unusable under synchronization contention while aggregate CPU remains deceptively low.

5. **CF-005 — Timeout Is Not Cancellation**  
   The caller abandons work, but downstream/server work continues and consumes capacity.

6. **CF-006 — Cold Recovery / Reconnect Herd**  
   Dependency recovery triggers synchronized reconnect/cache-warm traffic and a second outage.

Do not generalize infrastructure or runner abstractions from one experiment until CF-001 is complete and reviewed.

## 5. Locked Decisions

### D-001 — Product identity

CollapseLab is a **production failure-mechanics laboratory**.

### D-002 — Unit of value

The core unit is a reproducible experiment plus evidence, not an application feature.

### D-003 — Evidence standard

Claims require configuration-bound and revision-bound evidence.

### D-004 — Primary operational metrics

Prefer goodput, wasted work, amplification, and recovery behavior over raw RPS where the distinction matters.

### D-005 — v0.1 environment

Docker Compose first, local-only.

### D-006 — Initial SUT

Minimal Go HTTP service; experiment semantics stay framework-neutral.

### D-007 — Tooling strategy

Compose existing tools instead of replacing k6, Prometheus, OpenTelemetry, Toxiproxy, or similar specialist tools.

### D-008 — Lifecycle

Baseline, trigger, degradation, recovery, and verification are explicit phases.

### D-009 — Measurement integrity

`INVALID` measurement is a distinct outcome from a failed hypothesis.

### D-010 — Scope discipline

No Kubernetes, Kafka, service mesh, web UI, AI diagnosis, or distributed remote runner in v0.1 without a new scope gate.

### D-011 — Flagship set

v0.1 starts with exactly CF-001 through CF-006 above.

### D-012 — Research relationship

Paper/postmortem-derived experiments are labeled simplified **inspired-by** reproductions unless exact reproduction evidence exists.

## 6. Experiment Contract

Every experiment must define:

1. hypothesis,
2. stable baseline,
3. trigger,
4. observable failure,
5. propagation/amplification mechanism,
6. recovery behavior,
7. misleading metric or interpretation when relevant,
8. naive mitigation when relevant,
9. correct mitigation,
10. verification criteria,
11. exact evidence supporting the conclusion,
12. reproducibility instructions.

## 7. Result Semantics

Canonical experiment result states:

- **VALID + hypothesis supported**
- **VALID + hypothesis not supported**
- **INVALID measurement**

An invalid run must never be presented as a failed or supported systems hypothesis.

Examples of CF-001 invalidity:

- open arrival rate cannot be sustained,
- dropped iterations exceed the declared allowance,
- baseline rates are not comparable,
- telemetry contains a material gap around the trigger,
- load generator saturation is detected.

## 8. v0.1 Architecture

```text
                        local host
                            |
              +-------------+-------------+
              |                           |
        host_access bridge          internal lab network
        (management only)        (experiment data plane)
              |                           |
        +-----+------+           +--------+---------+
        |            |           |        |         |
       SUT       Prometheus      SUT   Prometheus   k6
       |              |           |
 public :8080      scrape         +-- sut-lab alias
 control:9091
```

Rules:

- host-published ports bind to `127.0.0.1`,
- k6 remains only on the internal `lab` network,
- SUT and Prometheus join `host_access` only for local management/readiness,
- experiment traffic and Prometheus scrape identity use the `sut-lab` alias on the internal network.

## 9. CF-001 Measurement Model

CF-001 uses the same pinned k6 binary in two modes:

- closed: `constant-vus`
- open: `constant-arrival-rate`

This avoids introducing tool-to-tool differences into the first proof.

The SUT has:

- deterministic base service time,
- deterministic global stall,
- separate public and control listeners,
- exact planned and actual trigger timestamps,
- Prometheus request-duration and in-flight metrics.

CF-001 must compare at minimum:

- p99 request latency,
- peak in-flight requests,
- achieved request rate,
- dropped iterations for the open workload,
- pre-trigger rate stability,
- recovery to the pre-trigger envelope.

## 10. Evidence Bundle Direction

Canonical run output:

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

Generated run bundles are ignored by Git except deliberately curated golden summaries.

## 11. Current Safety Boundary

As of the CF-001 infrastructure gate:

- public SUT listener: container `:8080`, host `127.0.0.1:18080`
- control listener: container `:9091`, host `127.0.0.1:19091`
- Prometheus: host `127.0.0.1:19090`
- k6 has no host-published listener and remains on the internal lab network.
- HTTP control scheduling is bounded and rejects overlapping stalls.

## 12. Success Criteria Before CF-001 Is Considered Complete

CF-001 is complete only when:

- configuration is strict and versioned,
- deterministic stall SUT is verified,
- local infrastructure gate is green,
- closed and open k6 workloads are explicit and inspectable,
- open workload validity checks detect generator saturation,
- exact trigger timing is captured,
- Prometheus evidence is collected around the trigger,
- validity is evaluated before hypothesis assertions,
- the run bundle is revision/tool-version bound,
- at least three clean canonical repetitions support the same mechanism,
- whole-branch review passes before merge.

## 13. Current Status

Completed and verified:

- Task 1 — configuration contract,
- Task 2 — deterministic SUT,
- Task 3 — Docker Compose + Prometheus infrastructure,
- Task 4 — explicit closed/open k6 workload semantics,
- Task 5 — evidence parsing and measurement-validity model.

Task 3 runtime evidence:

- GitHub Actions run `#5` / ID `35682953253`,
- head `80a9d1bd809bb84b0afd59c4c6e36911df737c0c`,
- Go 1.27.1 module closure: PASS,
- full Go race suite: PASS,
- Compose validation: PASS,
- image build/start: PASS,
- SUT and Prometheus readiness: PASS,
- Prometheus SUT target health: PASS,
- control-plane state: PASS,
- exact localhost port mappings: PASS,
- expected running service set: PASS,
- teardown with no residual containers: PASS.

Task 4 runtime evidence:

- TDD RED run: #7 / `35698257737`
- verified GREEN run: #8 / `35698455401`
- verified code head: `a9a39ead1c890c6c3cc6e508cfb66e52fc3fbfc5`
- pinned k6 inspect: PASS
- closed 5-VU / 30s workload: PASS
- open 100 iter/s / 100 max VU / 30s workload: PASS
- machine-readable summaries: PASS
- dropped-iteration evidence normalization: PASS

Task 5 verification evidence:

- parser/evaluator RED run: #11 / `35723020997`
- core parser/evaluator GREEN run: #12 / `35723333278`
- stall-resolution RED run: #13 / `35723612560`
- real generated-summary parser GREEN run: #16 / `35724005834`
- request-validity RED run: #17 / `35724363406`
- final full GREEN run: #18 / `35724503011`
- verified production head: `f938727465aa88e82dce1f2db332b3317b8b3e94`
- final Prometheus scrape interval: 100ms
- generated k6 closed/open summaries parsed by Go evidence parser: PASS
- HTTP failures and exact-204 check failures invalidate measurement before hypothesis evaluation: PASS
- INVALID / NOT_SUPPORTED / SUPPORTED ordering: PASS

Next implementation gate: **Task 6 — CF-001 runner and revision-bound evidence bundle**.
