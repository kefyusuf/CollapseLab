package cf001

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type EvaluationStatus string

const (
	EvaluationInvalid      EvaluationStatus = "INVALID"
	EvaluationSupported    EvaluationStatus = "SUPPORTED"
	EvaluationNotSupported EvaluationStatus = "NOT_SUPPORTED"
)

type K6Evidence struct {
	Scenario                 string
	IterationRate            float64
	WorkLatencyP99MS         float64
	DroppedIterationsPresent bool
	DroppedIterations        float64
	HTTPReqFailedRate        float64
	ChecksFailed             float64
}

type MetricPoint struct {
	At    time.Time
	Value float64
}

type PromSeries struct {
	Labels  map[string]string
	Samples []MetricPoint
}

type TriggerEvidence struct {
	Generation uint64
	Start      time.Time
	End        time.Time
}

type TrialEvidence struct {
	K6             K6Evidence
	RateSamples     []MetricPoint
	InflightSamples []MetricPoint
	Trigger         TriggerEvidence
}

type PairMetrics struct {
	ClosedP99MS           float64
	OpenP99MS             float64
	P99Ratio              float64
	ClosedPeakInflight    float64
	OpenPeakInflight      float64
	PeakInflightRatio     float64
	ClosedPreTriggerRate  float64
	OpenPreTriggerRate    float64
	ClosedPreTriggerCV    float64
	OpenPreTriggerCV      float64
	OpenAchievedRatio     float64
	ClosedRecovered       bool
	OpenRecovered         bool
	ClosedRecoveryAt      *time.Time
	OpenRecoveryAt        *time.Time
}

type PairEvaluation struct {
	Status            EvaluationStatus
	InvalidReasons    []string
	HypothesisReasons []string
	Metrics           PairMetrics
}

type k6SummaryEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	Scenario      string `json:"scenario"`
	Signals       struct {
		DroppedIterations *struct {
			Present bool    `json:"present"`
			Count   float64 `json:"count"`
		} `json:"dropped_iterations"`
		WorkLatency   map[string]float64 `json:"work_latency"`
		HTTPReqFailed map[string]float64 `json:"http_req_failed"`
		Checks        map[string]float64 `json:"checks"`
	} `json:"signals"`
	K6 struct {
		Metrics map[string]json.RawMessage `json:"metrics"`
	} `json:"k6"`
}

func ParseK6Summary(data []byte) (K6Evidence, error) {
	var envelope k6SummaryEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return K6Evidence{}, fmt.Errorf("decode k6 summary: %w", err)
	}

	if envelope.SchemaVersion != 1 {
		return K6Evidence{}, fmt.Errorf("k6 summary schema_version must be 1")
	}
	if envelope.Scenario != "closed" && envelope.Scenario != "open" {
		return K6Evidence{}, fmt.Errorf("k6 summary scenario must be closed or open")
	}
	if envelope.Signals.DroppedIterations == nil {
		return K6Evidence{}, fmt.Errorf("k6 summary missing dropped_iterations evidence")
	}

	p99, ok := envelope.Signals.WorkLatency["p(99)"]
	if !ok {
		return K6Evidence{}, fmt.Errorf("k6 summary missing work_latency p(99)")
	}
	if !isFinitePositive(p99) {
		return K6Evidence{}, fmt.Errorf("k6 summary work_latency p(99) must be finite and positive")
	}

	iterationsRaw, ok := envelope.K6.Metrics["iterations"]
	if !ok {
		return K6Evidence{}, fmt.Errorf("k6 summary missing iterations metric")
	}
	var iterations struct {
		Values struct {
			Rate float64 `json:"rate"`
		} `json:"values"`
	}
	if err := json.Unmarshal(iterationsRaw, &iterations); err != nil {
		return K6Evidence{}, fmt.Errorf("decode iterations metric: %w", err)
	}
	if !isFiniteNonNegative(iterations.Values.Rate) {
		return K6Evidence{}, fmt.Errorf("k6 summary iteration rate must be finite and non-negative")
	}

	dropped := envelope.Signals.DroppedIterations.Count
	if !isFiniteNonNegative(dropped) {
		return K6Evidence{}, fmt.Errorf("k6 summary dropped_iterations count must be finite and non-negative")
	}
	if !envelope.Signals.DroppedIterations.Present && dropped != 0 {
		return K6Evidence{}, fmt.Errorf("k6 summary dropped_iterations cannot be absent with non-zero count")
	}

	httpFailedRate, ok := envelope.Signals.HTTPReqFailed["rate"]
	if !ok {
		return K6Evidence{}, fmt.Errorf("k6 summary missing http_req_failed rate evidence")
	}
	if !isFiniteUnitInterval(httpFailedRate) {
		return K6Evidence{}, fmt.Errorf("k6 summary http_req_failed rate must be finite and in [0, 1]")
	}

	checksFailed, ok := envelope.Signals.Checks["fails"]
	if !ok {
		return K6Evidence{}, fmt.Errorf("k6 summary missing checks fails evidence")
	}
	if !isFiniteNonNegative(checksFailed) {
		return K6Evidence{}, fmt.Errorf("k6 summary checks fails must be finite and non-negative")
	}

	return K6Evidence{
		Scenario:                 envelope.Scenario,
		IterationRate:            iterations.Values.Rate,
		WorkLatencyP99MS:         p99,
		DroppedIterationsPresent: envelope.Signals.DroppedIterations.Present,
		DroppedIterations:        dropped,
		HTTPReqFailedRate:        httpFailedRate,
		ChecksFailed:             checksFailed,
	}, nil
}

func ParseTriggerEvidence(data []byte) (TriggerEvidence, error) {
	var raw struct {
		Generation      uint64     `json:"generation"`
		ActualStartedAt *time.Time `json:"actual_started_at"`
		ActualEndedAt   *time.Time `json:"actual_ended_at"`
		Active          bool       `json:"active"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TriggerEvidence{}, fmt.Errorf("decode trigger evidence: %w", err)
	}
	if raw.Generation == 0 {
		return TriggerEvidence{}, fmt.Errorf("trigger generation must be positive")
	}
	if raw.ActualStartedAt == nil || raw.ActualEndedAt == nil {
		return TriggerEvidence{}, fmt.Errorf("trigger evidence requires actual start and end")
	}
	if raw.Active {
		return TriggerEvidence{}, fmt.Errorf("trigger must be complete before evaluation")
	}
	if !raw.ActualEndedAt.After(*raw.ActualStartedAt) {
		return TriggerEvidence{}, fmt.Errorf("trigger end must be after start")
	}

	return TriggerEvidence{
		Generation: raw.Generation,
		Start:      raw.ActualStartedAt.UTC(),
		End:        raw.ActualEndedAt.UTC(),
	}, nil
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteUnitInterval(value float64) bool {
	return value >= 0 && value <= 1 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
