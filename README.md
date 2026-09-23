# CollapseLab

**Reproduce, observe, explain, and recover from production failure mechanics.**

CollapseLab is an evidence-first systems laboratory for failure modes that are easy to miss in ordinary load tests and architecture tutorials: coordinated omission, retry feedback loops, recovery storms, hidden contention, connection-budget collapse, timeout/cancellation mismatches, and related pathologies.

The repository is intentionally not a benchmark leaderboard and not a generic chaos-engineering platform. Each experiment is designed to answer a narrow systems question with a reproducible workload, explicit validity rules, revision-bound evidence, and a recovery story.

> **Current milestone:** CF-001 — Coordinated Omission.  
> The canonical repetition gate passes: the mechanism is reproducible, while the original 5× p99 hypothesis threshold is reproducibly **not supported** under the locked v0.1 profile.

## Why CollapseLab?

A system can look healthy while the useful behavior users experience is collapsing. A benchmark can also make the system look healthier by accidentally reducing offered load when responses slow down.

CollapseLab treats those problems as runnable experiments rather than prose-only advice.

The core experiment lifecycle is:

```text
baseline
  -> trigger
  -> propagation
  -> amplification
  -> measurement validity
  -> recovery
  -> hypothesis evaluation
  -> revision-bound evidence
```

Three rules drive the repository:

1. **Evidence before advice.** Performance and resilience claims must be tied to the exact revision, configuration, runtime identity, and raw measurements.
2. **INVALID is not failure.** Broken measurement cannot be interpreted as a failed systems hypothesis.
3. **Thresholds are not tuned after the fact.** If an experiment does not support its configured hypothesis, the result is preserved as evidence.

## Current experiment: CF-001 — Coordinated Omission

CF-001 compares two scheduling models against the same deterministic Go service:

- **closed model:** k6 `constant-vus`, 5 VUs;
- **open model:** k6 `constant-arrival-rate`, 100 iterations/s.

The service normally spends 50 ms per request. During each 30-second trial, CollapseLab schedules a real 500 ms global stall after traffic is established and records the *actual* stall start/end timestamps.

The closed workload waits for requests to finish before each VU can issue more work. During the stall, offered load therefore contracts. The open workload keeps scheduling arrivals independently of response completion, so work accumulates in flight and tail latency becomes visible.

See [the CF-001 experiment guide](experiments/cf-001-coordinated-omission/README.md) for the complete mechanism and validity model.

### Verified canonical repetition result

Task 7 ran three clean repetitions on the same revision/config/environment identity.

| Metric | Canonical result |
| --- | ---: |
| Closed p99 | 51.603–51.670 ms |
| Open p99 | 242.588–251.200 ms |
| Open/closed p99 ratio | **4.701–4.866×** |
| Peak in-flight ratio | **8.8–9.4×** |
| Open achieved-rate ratio | ~0.9986 |
| Closed recovery | 3/3 |
| Open recovery | 3/3 |
| Repetition gate | **PASS** |
| Hypothesis consensus | **ALL_NOT_SUPPORTED** |

The mechanism is reproducible. The original `p99 ratio >= 5×` criterion was met in 0/3 canonical runs; `open p99 >= 250 ms` was met in 1/3. Those thresholds were deliberately left unchanged.

The curated machine-readable result is committed at [docs/benchmarks/cf-001/task7-canonical-repetition.json](docs/benchmarks/cf-001/task7-canonical-repetition.json). Raw bundles stay out of Git.

## Architecture

```text
                         local host
                            |
              +-------------+-------------+
              |                           |
      host_access bridge             internal lab
       management plane              data plane
              |                           |
        +-----+------+           +--------+---------+
        |            |           |        |         |
       SUT       Prometheus      SUT   Prometheus   k6
       |                          |
 public :8080                     +-- sut-lab alias
control :9091
```

- Host-published ports bind only to `127.0.0.1`.
- k6 stays on the internal `lab` network.
- Prometheus scrapes the SUT through the internal `sut-lab` identity.
- The control listener is separate from the public work listener.
- Generated evidence under `runs/` is ignored by Git.

See [ARCHITECTURE.md](ARCHITECTURE.md) for trust boundaries, evidence flow, and extension rules.

## Quick start

### Prerequisites

The canonical local runner currently targets a Unix-like environment:

- Linux or WSL2;
- Go **1.27.1**;
- Docker Engine / Docker Desktop with Compose v2;
- Git;
- `make`, `curl`, and `id`.

Native Windows execution is not a supported canonical environment yet. On Windows, use WSL2 with Docker integration.

Canonical evidence also requires a **clean Git worktree**. Tracked or untracked non-ignored source changes cause the runner to refuse evidence generation.

### Verify the repository

```bash
go test -race ./...
go vet ./...
make compose-check
make lab-smoke
make lab-down
```

### Run three canonical CF-001 repetitions

```bash
go run ./cmd/cf001-repeat \
  --count 3 \
  --config experiments/cf-001-coordinated-omission/experiment.yaml \
  --runs-root runs/cf-001 \
  --summary runs/cf-001/repetition-summary.json \
  --report runs/cf-001/repetition-report.md \
  --repo-root .
```

The repetition runner starts from a clean lab state, executes three revision-bound closed/open experiment pairs, verifies every child bundle, then produces:

```text
runs/cf-001/
├── <run-1>/
├── <run-2>/
├── <run-3>/
├── repetition-summary.json
└── repetition-report.md
```

Each child run contains manifest-bound configuration, revision, environment, timeline, k6, Prometheus, assertions, and report evidence.

## Result semantics

CollapseLab intentionally distinguishes measurement integrity from the experiment hypothesis.

```text
raw evidence
    |
    v
measurement validity
    |
    +-- invalid -> INVALID
    |
    '-- valid
          |
          v
    hypothesis evaluation
       |           |
       v           v
  SUPPORTED   NOT_SUPPORTED
```

Examples of CF-001 invalidity include dropped open-model iterations, insufficient achieved arrival rate, request/check failures, telemetry gaps, incomparable pre-trigger baselines, or incomplete trigger evidence.

`NOT_SUPPORTED` means the measurement was valid but one or more configured hypothesis thresholds were not met.

## v0.1 scope

Implemented:

- deterministic Go SUT and stall gate;
- strict CF-001 configuration contract;
- closed/open k6 workloads;
- Prometheus evidence collection;
- measurement-validity model;
- revision-bound immutable run bundles;
- canonical three-run repetition gate.

Deliberately out of scope for v0.1:

- Kubernetes;
- Kafka or service mesh;
- remote/distributed runners;
- a browser UI;
- AI root-cause analysis;
- framework benchmark comparisons.

## Roadmap

The intended v0.1 flagship sequence is:

- **CF-001 — Coordinated Omission** — implemented;
- **CF-002 — Retry Metastability**;
- **CF-003 — Scale-Out Connection Budget Collapse**;
- **CF-004 — Low-CPU Lock Collapse**;
- **CF-005 — Timeout Is Not Cancellation**;
- **CF-006 — Cold Recovery / Reconnect Herd**.

New experiments should earn new infrastructure. CollapseLab does not add Kubernetes, brokers, or service decomposition merely to make the lab look more production-like.

## Repository map

```text
cmd/                         experiment CLIs
experiments/                 canonical experiment definitions
internal/cf001/              CF-001 contracts, evidence, runner, repetition gate
sut/api/                     minimal Go system under test
workloads/k6/                explicit workload semantics
observability/prometheus/    telemetry configuration
docs/benchmarks/             curated evidence summaries
docs/reviews/                whole-branch review records
docs/superpowers/            design and execution ledger
runs/                        generated raw evidence (gitignored)
```

## Safety and trust model

CollapseLab v0.1 is a **local trusted lab**, not a hostile multi-tenant sandbox.

- Host-facing ports are loopback-only.
- Fault injection is bounded and CF-001-specific.
- The repetition runner rejects dirty revisions.
- Git remote URL credentials are removed before revision evidence is persisted.
- Evidence verifiers reject path traversal and duplicate-run repetition claims.

The Docker containers themselves are trusted lab components. Container-to-container isolation is not presented as a security boundary.

## Limitations

- Canonical performance numbers are environment-specific, not universal hardware benchmarks.
- Version tags are recorded together with resolved local image IDs; registry digests are not yet pinned in source.
- CF-001 v1 intentionally locks workload-shape fields that are currently implemented as fixed runtime behavior.
- The full three-run repetition workflow is intentionally expensive and currently runs on every matching feature-branch push and pull request.
- Automated CodeRabbit CLI review could not be executed in the Task 8 environment because the CLI was unavailable and its installer host could not be resolved. No CodeRabbit findings are claimed.

## Project documents

- [Architecture](ARCHITECTURE.md)
- [CF-001 experiment guide](experiments/cf-001-coordinated-omission/README.md)
- [Task 7 canonical benchmark](docs/benchmarks/cf-001/task7-canonical-repetition.json)
- [v0.1 design specification](docs/superpowers/specs/2026-09-22-collapselab-v0.1-design.md)
- [CF-001 implementation ledger](docs/superpowers/plans/2026-09-22-cf-001-coordinated-omission.md)
- [Task 8 whole-branch review](docs/reviews/cf001-task8-review.md)
