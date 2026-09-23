package cf001

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseK6SummaryExtractsRequiredEvidence(t *testing.T) {
	input := []byte(`{
	  "schema_version": 1,
	  "scenario": "open",
	  "signals": {
	    "dropped_iterations": {"present": true, "count": 2},
	    "http_req_failed": {"rate": 0, "passes": 0, "fails": 2998},
	    "checks": {"rate": 1, "passes": 2998, "fails": 0},
	    "work_latency": {"avg": 55.2, "p(95)": 70.1, "p(99)": 420.5}
	  },
	  "k6": {
	    "metrics": {
	      "iterations": {"values": {"count": 2998, "rate": 99.6}}
	    }
	  }
	}`)

	got, err := ParseK6Summary(input)
	if err != nil {
		t.Fatal(err)
	}

	if got.Scenario != "open" {
		t.Fatalf("scenario = %q", got.Scenario)
	}
	if got.WorkLatencyP99MS != 420.5 {
		t.Fatalf("p99 = %v", got.WorkLatencyP99MS)
	}
	if got.IterationRate != 99.6 {
		t.Fatalf("iteration rate = %v", got.IterationRate)
	}
	if !got.DroppedIterationsPresent || got.DroppedIterations != 2 {
		t.Fatalf("dropped evidence = %+v", got)
	}
	if got.HTTPReqFailedRate != 0 || got.ChecksFailed != 0 {
		t.Fatalf("request evidence = %+v", got)
	}
}

func TestParseK6SummaryRejectsMissingP99(t *testing.T) {
	input := []byte(`{
	  "schema_version": 1,
	  "scenario": "closed",
	  "signals": {
	    "dropped_iterations": {"present": false, "count": 0},
	    "http_req_failed": {"rate": 0, "passes": 0, "fails": 2900},
	    "checks": {"rate": 1, "passes": 2900, "fails": 0},
	    "work_latency": {"avg": 50.0, "p(95)": 55.0}
	  },
	  "k6": {
	    "metrics": {
	      "iterations": {"values": {"count": 2900, "rate": 98.0}}
	    }
	  }
	}`)

	_, err := ParseK6Summary(input)
	if err == nil || !strings.Contains(err.Error(), "p(99)") {
		t.Fatalf("expected missing p99 error, got %v", err)
	}
}

func TestParsePrometheusMatrixRejectsNonFiniteSamples(t *testing.T) {
	input := []byte(`{
	  "status": "success",
	  "data": {
	    "resultType": "matrix",
	    "result": [
	      {
	        "metric": {"scenario": "open"},
	        "values": [[1760000000, "100"], [1760000001, "NaN"]]
	      }
	    ]
	  }
	}`)

	_, err := ParsePrometheusMatrix(input)
	if err == nil || !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("expected non-finite sample error, got %v", err)
	}
}

func TestParsePrometheusMatrixPreservesLabelsAndSamples(t *testing.T) {
	input := []byte(`{
	  "status": "success",
	  "data": {
	    "resultType": "matrix",
	    "result": [
	      {
	        "metric": {"scenario": "closed", "job": "collapselab-sut"},
	        "values": [[1760000000, "99.5"], [1760000001, "100.5"]]
	      }
	    ]
	  }
	}`)

	series, err := ParsePrometheusMatrix(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Samples) != 2 {
		t.Fatalf("unexpected matrix: %+v", series)
	}
	if series[0].Labels["scenario"] != "closed" {
		t.Fatalf("labels = %+v", series[0].Labels)
	}
	if series[0].Samples[1].Value != 100.5 {
		t.Fatalf("sample = %+v", series[0].Samples[1])
	}
}

func TestParseTriggerEvidenceRequiresCompletedWindow(t *testing.T) {
	start := time.Date(2026, 9, 22, 7, 0, 10, 0, time.UTC)
	end := start.Add(500 * time.Millisecond)
	payload, err := json.Marshal(map[string]any{
		"generation":        3,
		"actual_started_at": start,
		"actual_ended_at":   end,
		"active":            false,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseTriggerEvidence(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 3 || !got.Start.Equal(start) || !got.End.Equal(end) {
		t.Fatalf("trigger = %+v", got)
	}
}

func TestParseTriggerEvidenceRejectsIncompleteWindow(t *testing.T) {
	_, err := ParseTriggerEvidence([]byte(`{
	  "generation": 1,
	  "actual_started_at": "2026-09-22T07:00:10Z",
	  "actual_ended_at": null,
	  "active": true
	}`))
	if err == nil {
		t.Fatal("expected incomplete trigger window to be rejected")
	}
}

func TestEvaluatePairInvalidatesDroppedIterationsBeforeHypothesis(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.K6.DroppedIterationsPresent = true
	opened.K6.DroppedIterations = 1

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Status)
	}
	if len(result.InvalidReasons) == 0 {
		t.Fatal("expected invalidity reason")
	}
	if len(result.HypothesisReasons) != 0 {
		t.Fatalf("hypothesis must not be evaluated after invalidity: %+v", result.HypothesisReasons)
	}
}

func TestEvaluatePairInvalidatesInsufficientOpenArrivalRate(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.K6.IterationRate = 95

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Status)
	}
	if result.Metrics.OpenAchievedRatio >= cfg.Validity.MinimumOpenAchievedRatio {
		t.Fatalf("achieved ratio = %v", result.Metrics.OpenAchievedRatio)
	}
}

func TestEvaluatePairInvalidatesNonComparablePreTriggerRates(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.RateSamples = stableWindow(opened.Trigger.Start.Add(-5*time.Second), opened.Trigger.End.Add(8*time.Second), 80)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Status)
	}
}

func TestEvaluatePairInvalidatesUnstablePreTriggerRate(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.RateSamples = []MetricPoint{
		point(opened.Trigger.Start.Add(-5*time.Second), 70),
		point(opened.Trigger.Start.Add(-4*time.Second), 130),
		point(opened.Trigger.Start.Add(-3*time.Second), 70),
		point(opened.Trigger.Start.Add(-2*time.Second), 130),
		point(opened.Trigger.Start.Add(-1*time.Second), 70),
	}
	opened.RateSamples = append(opened.RateSamples,
		stableWindow(opened.Trigger.End, opened.Trigger.End.Add(8*time.Second), 100)...,
	)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Status)
	}
	if result.Metrics.OpenPreTriggerCV <= cfg.Validity.MaximumPretriggerRateCV {
		t.Fatalf("pre-trigger CV = %v", result.Metrics.OpenPreTriggerCV)
	}
}

func TestEvaluatePairInvalidatesMaterialTelemetryGap(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.RateSamples = []MetricPoint{
		point(opened.Trigger.Start.Add(-5*time.Second), 100),
		point(opened.Trigger.Start.Add(-4*time.Second), 100),
		point(opened.Trigger.Start.Add(-1*time.Second), 100),
		point(opened.Trigger.Start, 100),
	}
	opened.RateSamples = append(opened.RateSamples,
		stableWindow(opened.Trigger.End, opened.Trigger.End.Add(8*time.Second), 100)...,
	)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationInvalid {
		t.Fatalf("status = %q, want INVALID", result.Status)
	}
}

func TestEvaluatePairReturnsNotSupportedOnlyAfterValidMeasurement(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.K6.WorkLatencyP99MS = 200
	opened.InflightSamples = constantPoints(opened.Trigger.Start, opened.Trigger.End, 15)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationNotSupported {
		t.Fatalf("status = %q, want NOT_SUPPORTED; result=%+v", result.Status, result)
	}
	if len(result.InvalidReasons) != 0 {
		t.Fatalf("valid evidence marked invalid: %+v", result.InvalidReasons)
	}
	if len(result.HypothesisReasons) == 0 {
		t.Fatal("expected hypothesis reasons")
	}
}

func TestEvaluatePairReturnsSupportedWithDerivedMetrics(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status != EvaluationSupported {
		t.Fatalf("status = %q, result=%+v", result.Status, result)
	}
	if result.Metrics.P99Ratio < cfg.Hypothesis.MinimumP99RatioOpenOverClosed {
		t.Fatalf("p99 ratio = %v", result.Metrics.P99Ratio)
	}
	if result.Metrics.PeakInflightRatio < cfg.Hypothesis.MinimumPeakInflightRatioOpenOverClosed {
		t.Fatalf("inflight ratio = %v", result.Metrics.PeakInflightRatio)
	}
	if !result.Metrics.ClosedRecovered || !result.Metrics.OpenRecovered {
		t.Fatalf("expected both trials to recover: %+v", result.Metrics)
	}
}

func TestEvaluatePairTreatsObservedNonRecoveryAsValidBehavior(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)

	opened.RateSamples = append(
		stableWindow(opened.Trigger.Start.Add(-5*time.Second), opened.Trigger.Start, 100),
		[]MetricPoint{
			point(opened.Trigger.End, 40),
			point(opened.Trigger.End.Add(1*time.Second), 150),
			point(opened.Trigger.End.Add(2*time.Second), 40),
			point(opened.Trigger.End.Add(3*time.Second), 150),
			point(opened.Trigger.End.Add(4*time.Second), 40),
			point(opened.Trigger.End.Add(5*time.Second), 150),
			point(opened.Trigger.End.Add(6*time.Second), 40),
		}...,
	)

	result := EvaluatePair(cfg, closed, opened)

	if result.Status == EvaluationInvalid {
		t.Fatalf("observed non-recovery must not be measurement invalidity: %+v", result.InvalidReasons)
	}
	if result.Metrics.OpenRecovered {
		t.Fatalf("expected non-recovery to be preserved as behavior: %+v", result.Metrics)
	}
}

func validEvidencePair(cfg Config) (TrialEvidence, TrialEvidence) {
	base := time.Date(2026, 9, 22, 7, 0, 10, 0, time.UTC)
	closedStart := base
	openStart := base.Add(time.Minute)

	closed := TrialEvidence{
		K6: K6Evidence{
			Scenario:                 "closed",
			IterationRate:            98.5,
			WorkLatencyP99MS:         60,
			DroppedIterationsPresent: false,
			DroppedIterations:        0,
		},
		Trigger: TriggerEvidence{
			Generation: 1,
			Start:      closedStart,
			End:        closedStart.Add(cfg.Trial.StallDuration.Duration),
		},
		RateSamples: append(
			stableWindow(closedStart.Add(-5*time.Second), closedStart, 98.5),
			stableWindow(closedStart.Add(cfg.Trial.StallDuration.Duration), closedStart.Add(8*time.Second), 98.5)...,
		),
		InflightSamples: constantPoints(closedStart, closedStart.Add(cfg.Trial.StallDuration.Duration), 5),
	}

	opened := TrialEvidence{
		K6: K6Evidence{
			Scenario:                 "open",
			IterationRate:            100,
			WorkLatencyP99MS:         400,
			DroppedIterationsPresent: false,
			DroppedIterations:        0,
		},
		Trigger: TriggerEvidence{
			Generation: 1,
			Start:      openStart,
			End:        openStart.Add(cfg.Trial.StallDuration.Duration),
		},
		RateSamples: append(
			stableWindow(openStart.Add(-5*time.Second), openStart, 100),
			stableWindow(openStart.Add(cfg.Trial.StallDuration.Duration), openStart.Add(8*time.Second), 100)...,
		),
		InflightSamples: constantPoints(openStart, openStart.Add(cfg.Trial.StallDuration.Duration), 40),
	}

	return closed, opened
}

func stableWindow(start, end time.Time, value float64) []MetricPoint {
	var points []MetricPoint
	for at := start; !at.After(end); at = at.Add(time.Second) {
		points = append(points, point(at, value))
	}
	return points
}

func constantPoints(start, end time.Time, value float64) []MetricPoint {
	points := []MetricPoint{point(start, value)}
	if end.After(start) {
		points = append(points, point(end, value))
	}
	return points
}

func point(at time.Time, value float64) MetricPoint {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic("test helper requires finite value")
	}
	return MetricPoint{At: at, Value: value}
}


func TestGeneratedK6SummariesParse(t *testing.T) {
	dir := os.Getenv("CF001_SUMMARY_DIR")
	if dir == "" {
		t.Skip("CF001_SUMMARY_DIR is not set")
	}

	for _, scenario := range []string{"closed", "open"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(dir, scenario+"-summary.json")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read generated summary: %v", err)
			}

			evidence, err := ParseK6Summary(content)
			if err != nil {
				t.Fatalf("parse generated summary: %v", err)
			}
			if evidence.Scenario != scenario {
				t.Fatalf("scenario = %q, want %q", evidence.Scenario, scenario)
			}
			if evidence.WorkLatencyP99MS <= 0 {
				t.Fatalf("p99 = %v", evidence.WorkLatencyP99MS)
			}
			if evidence.IterationRate <= 0 {
				t.Fatalf("iteration rate = %v", evidence.IterationRate)
			}
		})
	}
}


func TestParseK6SummaryRejectsMissingHTTPAndCheckEvidence(t *testing.T) {
	input := []byte(`{
	  "schema_version": 1,
	  "scenario": "open",
	  "signals": {
	    "dropped_iterations": {"present": false, "count": 0},
	    "work_latency": {"p(99)": 400}
	  },
	  "k6": {
	    "metrics": {
	      "iterations": {"values": {"count": 3000, "rate": 100}}
	    }
	  }
	}`)

	_, err := ParseK6Summary(input)
	if err == nil {
		t.Fatal("expected missing HTTP/check evidence to be rejected")
	}
}

func TestEvaluatePairInvalidatesHTTPOrCheckFailuresBeforeHypothesis(t *testing.T) {
	cfg := validConfig()

	tests := []struct {
		name   string
		mutate func(*TrialEvidence)
	}{
		{
			name: "http request failure",
			mutate: func(evidence *TrialEvidence) {
				evidence.K6.HTTPReqFailedRate = 0.01
			},
		},
		{
			name: "204 contract check failure",
			mutate: func(evidence *TrialEvidence) {
				evidence.K6.ChecksFailed = 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			closed, opened := validEvidencePair(cfg)
			tt.mutate(&opened)

			result := EvaluatePair(cfg, closed, opened)

			if result.Status != EvaluationInvalid {
				t.Fatalf("status = %q, want INVALID; result=%+v", result.Status, result)
			}
			if len(result.HypothesisReasons) != 0 {
				t.Fatalf("hypothesis must not run for invalid request evidence: %+v", result.HypothesisReasons)
			}
		})
	}
}


func TestEvaluatePairBoundaryReasonPreservesDecisionPrecision(t *testing.T) {
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	opened.K6.WorkLatencyP99MS = 249.996

	result := EvaluatePair(cfg, closed, opened)
	if result.Status != EvaluationNotSupported {
		t.Fatalf("status = %q, want NOT_SUPPORTED; result=%+v", result.Status, result)
	}

	reasons := strings.Join(result.HypothesisReasons, "\n")
	if !strings.Contains(reasons, "open p99 249.996000ms below 250.000000ms") {
		t.Fatalf("boundary reason hides decision precision: %q", reasons)
	}
}
