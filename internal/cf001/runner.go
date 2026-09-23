package cf001

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type EnvironmentEvidence struct {
	GoVersion            string `json:"go_version,omitempty"`
	DockerVersion        string `json:"docker_version,omitempty"`
	DockerComposeVersion string `json:"docker_compose_version,omitempty"`
	K6Image              string `json:"k6_image,omitempty"`
	PrometheusImage      string `json:"prometheus_image,omitempty"`
	SUTImage             string `json:"sut_image,omitempty"`
	GOOS                 string `json:"goos,omitempty"`
	GOARCH               string `json:"goarch,omitempty"`
	NumCPU               int    `json:"num_cpu,omitempty"`
}

type TimelineEvent struct {
	At       time.Time       `json:"at"`
	Event    string          `json:"event"`
	Scenario string          `json:"scenario,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type CollectedTrial struct {
	K6Summary      []byte
	TriggerState   []byte
	RateMatrix     []byte
	InflightMatrix []byte
	Timeline       []TimelineEvent
}

type RevisionSource interface {
	Revision(context.Context) (RevisionEvidence, error)
}

type CF001Lab interface {
	Start(context.Context) (EnvironmentEvidence, error)
	CollectTrial(context.Context, string, Config, string) (CollectedTrial, error)
	Stop(context.Context) error
}

type RunnerOptions struct {
	ConfigPath string
	RunsRoot   string
	Now        func() time.Time
}

type RunnerDependencies struct {
	Revision RevisionSource
	Lab      CF001Lab
}

type RunResult struct {
	RunID      string
	BundleDir  string
	Evaluation PairEvaluation
}

type metricsDocument struct {
	Rate     json.RawMessage `json:"rate"`
	Inflight json.RawMessage `json:"inflight"`
}

type assertionsDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Status        EvaluationStatus `json:"status"`
	Invalid       []string       `json:"invalid_reasons,omitempty"`
	Hypothesis    []string       `json:"hypothesis_reasons,omitempty"`
	Metrics       PairMetrics    `json:"metrics"`
}

func RunCF001(ctx context.Context, opts RunnerOptions, deps RunnerDependencies) (result RunResult, retErr error) {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		return RunResult{}, errors.New("config path is required")
	}
	if strings.TrimSpace(opts.RunsRoot) == "" {
		return RunResult{}, errors.New("runs root is required")
	}
	if deps.Revision == nil {
		return RunResult{}, errors.New("revision source is required")
	}
	if deps.Lab == nil {
		return RunResult{}, errors.New("CF-001 lab is required")
	}

	configBytes, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return RunResult{}, fmt.Errorf("read config bytes: %w", err)
	}
	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return RunResult{}, err
	}

	revision, err := deps.Revision.Revision(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read revision evidence: %w", err)
	}
	if strings.TrimSpace(revision.Commit) == "" {
		return RunResult{}, errors.New("revision commit is required")
	}
	if revision.Dirty {
		return RunResult{}, errors.New("dirty revision cannot produce canonical evidence")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	createdAt := now().UTC()
	runID := RunIDAt(createdAt, revision.Commit)

	bundle, err := CreateRunBundle(opts.RunsRoot, runID)
	if err != nil {
		return RunResult{}, err
	}
	completed := false
	defer func() {
		if retErr != nil && !completed {
			_ = os.RemoveAll(bundle.Dir())
		}
	}()

	if err := bundle.Write("experiment.yaml", configBytes); err != nil {
		return RunResult{}, err
	}
	if err := bundle.WriteJSON("revision.json", revision); err != nil {
		return RunResult{}, err
	}

	timeline := []TimelineEvent{{
		At:    createdAt,
		Event: "run.created",
	}}

	environment, err := deps.Lab.Start(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("start lab: %w", err)
	}
	labStarted := true
	defer func() {
		if labStarted {
			stopErr := deps.Lab.Stop(context.Background())
			if retErr == nil && stopErr != nil {
				retErr = fmt.Errorf("stop lab: %w", stopErr)
			}
		}
	}()
	if err := bundle.WriteJSON("environment.json", environment); err != nil {
		return RunResult{}, err
	}
	timeline = append(timeline, TimelineEvent{
		At:    now().UTC(),
		Event: "lab.started",
	})

	evidenceByScenario := make(map[string]TrialEvidence, 2)
	for _, scenario := range []string{"closed", "open"} {
		collected, err := deps.Lab.CollectTrial(ctx, scenario, cfg, bundle.Dir())
		if err != nil {
			return RunResult{}, fmt.Errorf("collect %s trial: %w", scenario, err)
		}
		evidence, err := persistAndParseTrial(bundle, scenario, collected)
		if err != nil {
			return RunResult{}, fmt.Errorf("persist %s trial: %w", scenario, err)
		}
		evidenceByScenario[scenario] = evidence
		timeline = append(timeline, collected.Timeline...)
		timeline = append(timeline, TimelineEvent{
			At:       now().UTC(),
			Event:    "trial.collected",
			Scenario: scenario,
		})
	}

	if err := deps.Lab.Stop(ctx); err != nil {
		return RunResult{}, fmt.Errorf("stop lab: %w", err)
	}
	labStarted = false
	timeline = append(timeline, TimelineEvent{
		At:    now().UTC(),
		Event: "lab.stopped",
	})

	evaluation := EvaluatePair(cfg, evidenceByScenario["closed"], evidenceByScenario["open"])
	if err := bundle.WriteJSON("assertions.json", assertionsDocument{
		SchemaVersion: 1,
		Status:        evaluation.Status,
		Invalid:       evaluation.InvalidReasons,
		Hypothesis:    evaluation.HypothesisReasons,
		Metrics:       evaluation.Metrics,
	}); err != nil {
		return RunResult{}, err
	}
	if err := bundle.Write("timeline.jsonl", encodeTimeline(timeline)); err != nil {
		return RunResult{}, err
	}
	if err := bundle.Write("report.md", []byte(renderRunReport(runID, revision, evaluation))); err != nil {
		return RunResult{}, err
	}

	_, err = bundle.Finalize(ManifestMetadata{
		SchemaVersion:     1,
		ExperimentID:      cfg.ID,
		ExperimentVersion: cfg.Version,
		CreatedAt:         createdAt,
		RevisionSHA:       revision.Commit,
		ConfigSHA256:      SHA256Hex(configBytes),
	})
	if err != nil {
		return RunResult{}, err
	}
	if err := VerifyRunBundle(bundle.Dir()); err != nil {
		return RunResult{}, fmt.Errorf("verify finalized bundle: %w", err)
	}

	completed = true
	return RunResult{
		RunID:      runID,
		BundleDir:  bundle.Dir(),
		Evaluation: evaluation,
	}, nil
}

func persistAndParseTrial(bundle *RunBundle, scenario string, collected CollectedTrial) (TrialEvidence, error) {
	if scenario != "closed" && scenario != "open" {
		return TrialEvidence{}, fmt.Errorf("unsupported scenario %q", scenario)
	}

	if err := bundle.Write(filepath.ToSlash(filepath.Join(scenario, "k6-summary.json")), collected.K6Summary); err != nil {
		return TrialEvidence{}, err
	}
	if err := bundle.WriteJSON(filepath.ToSlash(filepath.Join("metrics", scenario+".json")), metricsDocument{
		Rate:     json.RawMessage(collected.RateMatrix),
		Inflight: json.RawMessage(collected.InflightMatrix),
	}); err != nil {
		return TrialEvidence{}, err
	}

	k6, err := ParseK6Summary(collected.K6Summary)
	if err != nil {
		return TrialEvidence{}, err
	}
	if k6.Scenario != scenario {
		return TrialEvidence{}, fmt.Errorf("k6 scenario %q does not match %q", k6.Scenario, scenario)
	}

	trigger, err := ParseTriggerEvidence(collected.TriggerState)
	if err != nil {
		return TrialEvidence{}, err
	}
	rateSeries, err := ParsePrometheusMatrix(collected.RateMatrix)
	if err != nil {
		return TrialEvidence{}, err
	}
	inflightSeries, err := ParsePrometheusMatrix(collected.InflightMatrix)
	if err != nil {
		return TrialEvidence{}, err
	}

	rateSamples, err := samplesForScenario(rateSeries, scenario)
	if err != nil {
		return TrialEvidence{}, fmt.Errorf("rate evidence: %w", err)
	}
	inflightSamples, err := samplesForScenario(inflightSeries, scenario)
	if err != nil {
		return TrialEvidence{}, fmt.Errorf("in-flight evidence: %w", err)
	}

	return TrialEvidence{
		K6:             k6,
		RateSamples:     rateSamples,
		InflightSamples: inflightSamples,
		Trigger:         trigger,
	}, nil
}

func samplesForScenario(series []PromSeries, scenario string) ([]MetricPoint, error) {
	var matched []PromSeries
	for _, current := range series {
		if current.Labels["scenario"] == scenario {
			matched = append(matched, current)
		}
	}
	if len(matched) != 1 {
		return nil, fmt.Errorf("expected exactly one %q series, got %d", scenario, len(matched))
	}
	return matched[0].Samples, nil
}

func RunIDAt(at time.Time, revision string) string {
	revision = strings.TrimSpace(revision)
	prefix := revision
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		prefix = "unknown"
	}
	return at.UTC().Format("20060102T150405Z") + "-" + prefix
}

func encodeTimeline(events []TimelineEvent) []byte {
	events = append([]TimelineEvent(nil), events...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].At.Before(events[j].At)
	})

	var buffer bytes.Buffer
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		buffer.Write(data)
		buffer.WriteByte('\n')
	}
	return buffer.Bytes()
}

func renderRunReport(runID string, revision RevisionEvidence, evaluation PairEvaluation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CF-001 Run %s\n\n", runID)
	fmt.Fprintf(&b, "- Revision: `%s`\n", revision.Commit)
	fmt.Fprintf(&b, "- Evaluation status: **%s**\n", evaluation.Status)
	fmt.Fprintf(&b, "- Closed p99: %.2f ms\n", evaluation.Metrics.ClosedP99MS)
	fmt.Fprintf(&b, "- Open p99: %.2f ms\n", evaluation.Metrics.OpenP99MS)
	fmt.Fprintf(&b, "- p99 ratio: %.2f\n", evaluation.Metrics.P99Ratio)
	fmt.Fprintf(&b, "- Peak in-flight ratio: %.2f\n", evaluation.Metrics.PeakInflightRatio)
	if len(evaluation.InvalidReasons) > 0 {
		b.WriteString("\n## Invalidity reasons\n\n")
		for _, reason := range evaluation.InvalidReasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
	}
	if len(evaluation.HypothesisReasons) > 0 {
		b.WriteString("\n## Hypothesis reasons\n\n")
		for _, reason := range evaluation.HypothesisReasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
	}
	return b.String()
}
