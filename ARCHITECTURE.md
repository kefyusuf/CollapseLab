# CollapseLab Architecture

This document describes the v0.1 architecture and trust boundaries of CollapseLab. The current implementation is intentionally CF-001-first: abstractions are extracted only when a verified experiment proves they are shared.

## Design intent

CollapseLab optimizes for three properties:

1. **causal visibility** — the failure mechanism must remain understandable;
2. **measurement integrity** — invalid evidence must be rejected before hypothesis evaluation;
3. **reproducibility** — evidence must identify the revision, configuration, environment, and artifacts that produced a result.

It does not optimize for production feature completeness.

## Runtime topology

```text
                           host
                            |
            +---------------+---------------+
            |                               |
     127.0.0.1 ports                  Docker networks
            |                               |
     +------+------+                 +------+------+
     |             |                 |             |
  :18080         :19090            lab        host_access
   public       Prometheus        internal        bridge
  :19091                            |               |
  control                  +--------+--------+      |
                           |        |        |      |
                          SUT  Prometheus    k6     |
                           |        |               |
                           +--------+---------------+
                                    |
                              management access
```

### System under test

The SUT is a minimal Go HTTP service.

Public listener, container port `:8080`:

- `GET /work` — deterministic work endpoint;
- `GET /healthz`;
- `GET /metrics`.

Control listener, container port `:9091`:

- `POST /__control/stall`;
- `GET /__control/state`.

The listeners are intentionally separate so control operations do not share the public workload route surface.

### Networks

`lab`:

- Docker `internal: true`;
- carries experiment traffic;
- k6 is attached only here;
- SUT has the stable alias `sut-lab`;
- Prometheus scrapes `http://sut-lab:8080/metrics`.

`host_access`:

- bridge used for local management/readiness;
- SUT and Prometheus join it;
- host ports bind to `127.0.0.1`.

This arrangement is a local experiment topology, not a hostile-container security boundary.

## CF-001 execution model

The canonical workload shape is fixed for CF-001 v1:

```text
base service time    50 ms
work path            /work
trial duration       30 s
closed executor      constant-vus
closed VUs           5
open executor        constant-arrival-rate
open rate            100/s
open preallocated    100 VUs
open max VUs         100
```

Those values are now validated as canonical because the SUT/Compose/k6 runtime implements them explicitly. Allowing the YAML to claim different values while the runtime ignored them would break evidence binding.

Trigger/stall and evaluation fields remain configuration-backed where the runtime actually consumes them, subject to safety/capacity validation.

## Stall lifecycle

```text
schedule request
      |
      v
planned start/end
      |
      v
actual stall starts
      |
      +--> incoming /work handlers wait
      |
      v
actual stall ends
      |
      v
blocked requests released
```

Evidence uses **actual** start/end timestamps, not the runner's requested schedule timestamps.

Only one stall can be active/scheduled at a time.

## Closed vs open workload semantics

Closed:

```text
5 VUs
request -> wait response -> next request
```

A server stall reduces how quickly those VUs can offer new work.

Open:

```text
100 arrivals/s
arrival scheduling independent of response completion
```

During a stall, in-flight work can accumulate. `maxVUs` is deliberately fixed to the preallocated value so insufficient generator capacity appears as `dropped_iterations` rather than hidden worker-pool growth.

## Telemetry

Prometheus scrapes the SUT every 100 ms with a 90 ms timeout.

Primary CF-001 signals:

- `collapselab_requests_total{scenario=...}`;
- `collapselab_request_duration_seconds{scenario=...}`;
- `collapselab_inflight_requests{scenario=...}`;
- `collapselab_stall_active`;
- `collapselab_stalls_total`.

k6 separately records `work_latency`, request-failure evidence, exact-204 check evidence, iterations, and dropped iterations.

The two evidence planes have different roles:

- k6 represents client-observed workload behavior;
- Prometheus represents SUT-side request rate/in-flight behavior.

## Evidence pipeline

```text
experiment.yaml
     |
     v
strict Config.Validate
     |
     v
GitRevisionSource
  clean revision?
     |
     v
DockerCF001Lab
     |
     +--> closed trial --> k6 + Prometheus + trigger state
     |
     '--> open trial   --> k6 + Prometheus + trigger state
                                  |
                                  v
                             Parse evidence
                                  |
                                  v
                          measurement validity
                            |             |
                         INVALID        valid
                                          |
                                          v
                                  hypothesis evaluation
                                          |
                                  SUPPORTED / NOT_SUPPORTED
                                          |
                                          v
                                  immutable run bundle
                                          |
                                          v
                                  manifest SHA-256
```

## Run-bundle integrity

A finalized run stores:

```text
manifest.json
environment.json
experiment.yaml
revision.json
timeline.jsonl
closed/k6-summary.json
open/k6-summary.json
metrics/closed.json
metrics/open.json
assertions.json
report.md
```

The manifest records the exact revision/config identity and SHA-256 plus byte size for each evidence artifact.

Verification rejects:

- dirty revisions;
- revision/manifest mismatch;
- config digest mismatch;
- missing or altered artifact bytes;
- symlinked bundle entries;
- invalid run IDs/path traversal.

The repetition layer additionally rejects duplicate child run IDs and requires all child bundles to agree on revision, config, and environment identity.

## Revision evidence

Canonical evidence requires a clean Git worktree.

Tracked changes and untracked non-ignored source files mark the revision dirty. Generated `runs/` evidence is ignored by Git and does not invalidate the revision.

The stored origin URL is sanitized before persistence: URL userinfo is removed so credentials are not written to evidence artifacts.

## Repetition gate

Task 7 executes at least three complete CF-001 run pairs.

The repetition gate checks:

- all measurements are valid;
- revision/config/environment identities match;
- run IDs are distinct;
- pre-trigger baselines remain comparable;
- open arrival delivery remains above the configured validity floor;
- p99 amplification remains in the same direction;
- in-flight amplification remains in the same direction;
- closed/open recovery succeeds consistently.

This gate is deliberately separate from hypothesis consensus. A reproducible mechanism may produce `ALL_NOT_SUPPORTED` when configured amplitude thresholds are too high.

## Trust boundaries

v0.1 assumes:

- the developer controls the local host;
- Docker images and lab containers are trusted experiment components;
- k6 scripts in the repository are trusted;
- localhost callers are trusted.

It does **not** claim:

- hostile-container isolation;
- untrusted remote target support;
- multi-user execution isolation;
- signed evidence authenticity.

Manifest hashes provide tamper detection/self-consistency, not cryptographic authorship. GitHub Actions artifact digests can provide an additional external transport anchor for canonical CI runs.

## Extension rules

Do not generalize a component merely because a second implementation seems likely.

A future experiment should reuse CF-001 primitives only when its semantics match. In particular:

- failure-specific configuration stays experiment-specific;
- validity rules stay experiment-specific;
- generic evidence primitives may be extracted after repeated use;
- Kubernetes, brokers, service mesh, remote runners, or a UI require a separate scope decision.

## Known operational limitations

- Local canonical execution currently requires Linux/WSL2-style `id -u/-g`.
- Docker image source uses version tags; resolved image IDs are persisted in environment evidence, but source-level registry digests are not yet pinned.
- Prometheus range-query samples demonstrate the query result surface, not a cryptographically complete raw scrape log.
- The full three-run regression workflow is intentionally expensive and currently executes on every matching feature-branch push and pull request.
