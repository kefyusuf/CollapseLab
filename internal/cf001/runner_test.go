package cf001

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeRevisionSource struct {
	revision RevisionEvidence
	err      error
}

func (f fakeRevisionSource) Revision(context.Context) (RevisionEvidence, error) {
	return f.revision, f.err
}

type fakeCF001Lab struct {
	started   bool
	stopped   bool
	scenarios []string
	trials    map[string]CollectedTrial
}

func (f *fakeCF001Lab) Start(context.Context) (EnvironmentEvidence, error) {
	f.started = true
	return EnvironmentEvidence{
		GoVersion:            "go1.27.1",
		DockerVersion:        "28.0.0",
		DockerComposeVersion: "2.38.2",
		K6Image:              "grafana/k6:2.2.0",
		PrometheusImage:      "prom/prometheus:v3.14.0",
	}, nil
}

func (f *fakeCF001Lab) CollectTrial(_ context.Context, scenario string, _ Config, _ string) (CollectedTrial, error) {
	f.scenarios = append(f.scenarios, scenario)
	return f.trials[scenario], nil
}

func (f *fakeCF001Lab) Stop(context.Context) error {
	f.stopped = true
	return nil
}

func TestRunnerRejectsDirtyRevisionBeforeLabStart(t *testing.T) {
	configPath := writeTempConfig(t, validYAML)
	lab := &fakeCF001Lab{}

	_, err := RunCF001(context.Background(), RunnerOptions{
		ConfigPath: configPath,
		RunsRoot:  t.TempDir(),
		Now:        func() time.Time { return time.Date(2026, 9, 22, 20, 30, 0, 0, time.UTC) },
	}, RunnerDependencies{
		Revision: fakeRevisionSource{revision: RevisionEvidence{
			Commit: strings.Repeat("a", 40),
			Dirty:  true,
		}},
		Lab: lab,
	})

	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("expected dirty revision error, got %v", err)
	}
	if lab.started {
		t.Fatal("lab must not start for a dirty revision")
	}
}

func TestRunnerCreatesRevisionBoundBundleAndOrdersTrials(t *testing.T) {
	configPath := writeTempConfig(t, validYAML)
	revision := RevisionEvidence{
		Commit:     strings.Repeat("b", 40),
		Branch:     "feat/cf-001-coordinated-omission",
		Repository: "https://github.com/kefyusuf/CollapseLab",
		Dirty:      false,
	}
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	lab := &fakeCF001Lab{trials: map[string]CollectedTrial{
		"closed": collectedTrialFixture(t, closed),
		"open":   collectedTrialFixture(t, opened),
	}}

	result, err := RunCF001(context.Background(), RunnerOptions{
		ConfigPath: configPath,
		RunsRoot:  t.TempDir(),
		Now:        func() time.Time { return time.Date(2026, 9, 22, 20, 30, 0, 0, time.UTC) },
	}, RunnerDependencies{
		Revision: fakeRevisionSource{revision: revision},
		Lab:      lab,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !lab.started || !lab.stopped {
		t.Fatalf("lab lifecycle incomplete: started=%v stopped=%v", lab.started, lab.stopped)
	}
	if want := []string{"closed", "open"}; !reflect.DeepEqual(lab.scenarios, want) {
		t.Fatalf("scenario order = %v, want %v", lab.scenarios, want)
	}
	if result.Evaluation.Status != EvaluationSupported {
		t.Fatalf("evaluation status = %q", result.Evaluation.Status)
	}
	if err := VerifyRunBundle(result.BundleDir); err != nil {
		t.Fatalf("verify generated bundle: %v", err)
	}

	expectedFiles := []string{
		"manifest.json",
		"environment.json",
		"experiment.yaml",
		"revision.json",
		"timeline.jsonl",
		"closed/k6-summary.json",
		"open/k6-summary.json",
		"metrics/closed.json",
		"metrics/open.json",
		"assertions.json",
		"report.md",
	}
	for _, rel := range expectedFiles {
		if _, err := os.Stat(filepath.Join(result.BundleDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	bundledConfig, err := os.ReadFile(filepath.Join(result.BundleDir, "experiment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bundledConfig) != string(configBytes) {
		t.Fatal("bundle did not preserve exact experiment config bytes")
	}

	manifestData, err := os.ReadFile(filepath.Join(result.BundleDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RevisionSHA != revision.Commit {
		t.Fatalf("manifest revision = %s", manifest.RevisionSHA)
	}
	if manifest.ConfigSHA256 != SHA256Hex(configBytes) {
		t.Fatalf("manifest config hash = %s", manifest.ConfigSHA256)
	}
}

func TestRunnerPersistsInvalidMeasurementWithoutExecutionFailure(t *testing.T) {
	configPath := writeTempConfig(t, validYAML)
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.K6.DroppedIterationsPresent = true
	opened.K6.DroppedIterations = 1

	lab := &fakeCF001Lab{trials: map[string]CollectedTrial{
		"closed": collectedTrialFixture(t, closed),
		"open":   collectedTrialFixture(t, opened),
	}}

	result, err := RunCF001(context.Background(), RunnerOptions{
		ConfigPath: configPath,
		RunsRoot:  t.TempDir(),
		Now:        func() time.Time { return time.Date(2026, 9, 22, 20, 31, 0, 0, time.UTC) },
	}, RunnerDependencies{
		Revision: fakeRevisionSource{revision: RevisionEvidence{
			Commit: strings.Repeat("c", 40),
			Dirty:  false,
		}},
		Lab: lab,
	})
	if err != nil {
		t.Fatalf("invalid experiment result is not an execution error: %v", err)
	}
	if result.Evaluation.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Evaluation.Status)
	}
	if len(result.Evaluation.HypothesisReasons) != 0 {
		t.Fatalf("invalid evidence must not contain hypothesis reasons: %+v", result.Evaluation.HypothesisReasons)
	}
	if err := VerifyRunBundle(result.BundleDir); err != nil {
		t.Fatalf("invalid result bundle must still verify: %v", err)
	}
}

func TestRunIDUsesUTCAndRevisionPrefix(t *testing.T) {
	got := RunIDAt(
		time.Date(2026, 9, 22, 23, 30, 45, 123, time.FixedZone("local", 3*60*60)),
		"abcdef1234567890",
	)
	if got != "20260922T203045Z-abcdef12" {
		t.Fatalf("run id = %q", got)
	}
}

func collectedTrialFixture(t *testing.T, evidence TrialEvidence) CollectedTrial {
	t.Helper()

	summary := map[string]any{
		"schema_version": 1,
		"scenario":       evidence.K6.Scenario,
		"signals": map[string]any{
			"dropped_iterations": map[string]any{
				"present": evidence.K6.DroppedIterationsPresent,
				"count":   evidence.K6.DroppedIterations,
			},
			"http_req_failed": map[string]any{"rate": evidence.K6.HTTPReqFailedRate},
			"checks":          map[string]any{"fails": evidence.K6.ChecksFailed},
			"work_latency":    map[string]any{"p(99)": evidence.K6.WorkLatencyP99MS},
		},
		"k6": map[string]any{
			"metrics": map[string]any{
				"iterations": map[string]any{
					"values": map[string]any{
						"count": 3000,
						"rate":  evidence.K6.IterationRate,
					},
				},
			},
		},
	}

	trigger := map[string]any{
		"generation":        evidence.Trigger.Generation,
		"actual_started_at": evidence.Trigger.Start,
		"actual_ended_at":   evidence.Trigger.End,
		"active":            false,
	}

	return CollectedTrial{
		K6Summary:     mustJSON(t, summary),
		TriggerState:  mustJSON(t, trigger),
		RateMatrix:    prometheusMatrixFixture(t, evidence.K6.Scenario, evidence.RateSamples),
		InflightMatrix: prometheusMatrixFixture(t, evidence.K6.Scenario, evidence.InflightSamples),
		Timeline: []TimelineEvent{
			{
				At:       evidence.Trigger.Start,
				Event:    "stall.started",
				Scenario: evidence.K6.Scenario,
			},
			{
				At:       evidence.Trigger.End,
				Event:    "stall.ended",
				Scenario: evidence.K6.Scenario,
			},
		},
	}
}

func prometheusMatrixFixture(t *testing.T, scenario string, samples []MetricPoint) []byte {
	t.Helper()
	// Prometheus values are encoded as [unix-seconds, "value"].
	encodedValues := make([][]any, 0, len(samples))
	for _, sample := range samples {
		encodedValues = append(encodedValues, []any{
			float64(sample.At.UnixNano()) / float64(time.Second),
			formatFloat(sample.Value),
		})
	}

	return mustJSON(t, map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "matrix",
			"result": []any{
				map[string]any{
					"metric": map[string]string{
						"scenario": scenario,
						"job":      "collapselab-sut",
					},
					"values": encodedValues,
				},
			},
		},
	})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func formatFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(
		strconv.FormatFloat(value, 'f', 6, 64),
		"0",
	), ".")
}


func TestRunReportLabelsEvaluationStatusWithoutMisclassifyingMeasurement(t *testing.T) {
	report := renderRunReport(
		"run-001",
		RevisionEvidence{Commit: strings.Repeat("a", 40)},
		PairEvaluation{Status: EvaluationNotSupported},
	)

	if !strings.Contains(report, "- Evaluation status: **NOT_SUPPORTED**") {
		t.Fatalf("report does not label evaluation status correctly:\n%s", report)
	}
	if strings.Contains(report, "Measurement status:") {
		t.Fatalf("report misclassifies evaluation result as measurement status:\n%s", report)
	}
}
