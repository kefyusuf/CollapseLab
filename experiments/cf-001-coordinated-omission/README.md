# CF-001 — Coordinated Omission

CF-001 demonstrates how a closed workload can under-report tail latency during a deterministic service stall because the load generator itself reduces offered work while it waits for slow responses.

The same SUT is then exercised with an open arrival workload that keeps scheduling work independently of response completion.

## Question

> Can a benchmark report a healthy-looking p99 while a fixed-rate arrival process reveals a materially larger tail and in-flight queue during the same server-side stall?

CF-001 answers this with executable traffic, actual stall timestamps, client-side latency, SUT-side in-flight telemetry, strict validity checks, and revision-bound evidence.

## Canonical profile

```yaml
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
```

CF-001 v1 locks the workload-shape fields that are implemented as fixed runtime behavior. This prevents a bundle from claiming it ran a different service time, path, duration, VU count, or arrival rate when the underlying Compose/k6 runtime did not actually change.

The exact canonical source of truth is [experiment.yaml](experiment.yaml).

## Mechanism

### Closed model

```text
VU 1: request -------- wait -------- next
VU 2: request -------- wait -------- next
...
```

When the service stalls, each VU stops producing new requests until its current request finishes. Offered work contracts at the same time latency grows.

### Open model

```text
time --->

arrival  arrival  arrival  arrival  arrival
   |        |        |        |        |
   +--------+--------+--------+--------+
                    SUT
```

Arrivals continue at 100/s. During the 500 ms stall, work accumulates in flight instead of disappearing from the offered-load stream.

That difference is the coordinated-omission mechanism this experiment makes visible.

## Deterministic trigger

The runner schedules the stall only after workload traffic is observed.

It records:

- schedule time;
- planned start/end;
- actual start/end;
- workload start/end.

Evaluation is anchored to the actual trigger window.

## Measurement validity

CF-001 evaluates measurement validity before the hypothesis.

A run is `INVALID` when required evidence cannot support a conclusion. Current examples include:

- dropped open-model iterations above the allowance;
- achieved open arrival rate below 99%;
- HTTP request failures;
- failed exact-204 checks;
- unstable/incomparable pre-trigger rates;
- incomplete trigger evidence;
- insufficient required telemetry;
- invalid/non-finite evidence.

Only a valid run can become `SUPPORTED` or `NOT_SUPPORTED`.

## Hypothesis

The original v0.1 hypothesis requires all of the following:

- closed p99 <= 150 ms;
- open p99 >= 250 ms;
- open/closed p99 ratio >= 5×;
- open/closed peak in-flight ratio >= 5×.

These are experiment thresholds, not universal performance targets.

## Canonical Task 7 result

Three revision/config/environment-identical repetitions produced:

| Run | Evaluation | Closed p99 | Open p99 | p99 ratio | Peak in-flight ratio |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | NOT_SUPPORTED | 51.603 ms | 242.588 ms | 4.701× | 9.4× |
| 2 | NOT_SUPPORTED | 51.670 ms | 244.522 ms | 4.732× | 9.0× |
| 3 | NOT_SUPPORTED | 51.620 ms | 251.200 ms | 4.866× | 8.8× |

Repetition gate: **PASS**  
Hypothesis consensus: **ALL_NOT_SUPPORTED**

Interpretation:

- the coordinated-omission mechanism is reproducible;
- in-flight amplification is strong and stable;
- client p99 amplification is stable around 4.7–4.9×;
- the 5× p99-ratio threshold was met in 0/3 runs;
- the 250 ms open-p99 threshold was met in 1/3 runs.

The threshold was not lowered after seeing the result.

Curated evidence: [../../docs/benchmarks/cf-001/task7-canonical-repetition.json](../../docs/benchmarks/cf-001/task7-canonical-repetition.json)

## Run the experiment

Prerequisites are documented in the repository [README](../../README.md).

From a clean Git worktree:

```bash
go test -race ./...
go vet ./...
make compose-check
```

Then run three complete canonical repetitions:

```bash
go run ./cmd/cf001-repeat \
  --count 3 \
  --config experiments/cf-001-coordinated-omission/experiment.yaml \
  --runs-root runs/cf-001 \
  --summary runs/cf-001/repetition-summary.json \
  --report runs/cf-001/repetition-report.md \
  --repo-root .
```

Generated raw evidence is written beneath `runs/cf-001/` and is intentionally gitignored.

## Child bundle contents

Each repetition contains:

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

`manifest.json` binds artifact byte sizes and SHA-256 digests to the exact revision and config digest.

## What this experiment does not prove

CF-001 does not establish that:

- all closed-loop load tests are wrong;
- 4.7× is a universal coordinated-omission factor;
- Go is faster/slower than another runtime;
- the current numbers transfer to another host;
- the 5× threshold should automatically be changed to match observed data.

It demonstrates one controlled failure-measurement mechanism under one canonical local profile.

## Why the hypothesis can be NOT_SUPPORTED while the experiment succeeds

The experiment has two separate questions:

1. **Was the measurement valid and the mechanism reproduced?**
2. **Did every configured threshold pass?**

For Task 7 the answers were:

```text
mechanism/repetition gate = PASS
hypothesis consensus      = ALL_NOT_SUPPORTED
```

That is a useful scientific outcome, not a failed implementation.

## Next research questions

Separate future design decisions may examine:

- whether the hypothesis should be reframed around distributions rather than a 5× binary threshold;
- how the effect scales with stall duration or arrival rate;
- coordinated omission across multi-hop fan-out;
- workload deadline/goodput semantics.

Those are not silently folded into CF-001 v1.
