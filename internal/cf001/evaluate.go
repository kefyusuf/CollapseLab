package cf001

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	preTriggerWindow      = 5 * time.Second
	rateEdgeTolerance     = 1500 * time.Millisecond
	maxRateSampleGap      = 2 * time.Second
)

type windowStats struct {
	Mean float64
	CV   float64
}

func EvaluatePair(cfg Config, closed, opened TrialEvidence) PairEvaluation {
	result := PairEvaluation{
		Metrics: PairMetrics{
			ClosedP99MS: closed.K6.WorkLatencyP99MS,
			OpenP99MS:   opened.K6.WorkLatencyP99MS,
		},
	}

	if err := cfg.Validate(); err != nil {
		result.InvalidReasons = append(result.InvalidReasons, "configuration invalid: "+err.Error())
		result.Status = EvaluationInvalid
		return result
	}

	validateTrialBasics("closed", closed, &result)
	validateTrialBasics("open", opened, &result)

	if cfg.Open.RateRPS > 0 && isFiniteNonNegative(opened.K6.IterationRate) {
		result.Metrics.OpenAchievedRatio = opened.K6.IterationRate / float64(cfg.Open.RateRPS)
		if result.Metrics.OpenAchievedRatio < cfg.Validity.MinimumOpenAchievedRatio {
			result.InvalidReasons = append(result.InvalidReasons,
				fmt.Sprintf("open achieved ratio %.4f below minimum %.4f",
					result.Metrics.OpenAchievedRatio,
					cfg.Validity.MinimumOpenAchievedRatio,
				),
			)
		}
	}

	if opened.K6.DroppedIterations > float64(cfg.Validity.MaxDroppedIterations) {
		result.InvalidReasons = append(result.InvalidReasons,
			fmt.Sprintf("open dropped iterations %.0f exceed maximum %d",
				opened.K6.DroppedIterations,
				cfg.Validity.MaxDroppedIterations,
			),
		)
	}

	closedStats, err := preTriggerStats(closed)
	if err != nil {
		result.InvalidReasons = append(result.InvalidReasons, "closed pre-trigger telemetry: "+err.Error())
	} else {
		result.Metrics.ClosedPreTriggerRate = closedStats.Mean
		result.Metrics.ClosedPreTriggerCV = closedStats.CV
		if closedStats.CV > cfg.Validity.MaximumPretriggerRateCV {
			result.InvalidReasons = append(result.InvalidReasons,
				fmt.Sprintf("closed pre-trigger CV %.4f exceeds maximum %.4f",
					closedStats.CV,
					cfg.Validity.MaximumPretriggerRateCV,
				),
			)
		}
	}

	openStats, err := preTriggerStats(opened)
	if err != nil {
		result.InvalidReasons = append(result.InvalidReasons, "open pre-trigger telemetry: "+err.Error())
	} else {
		result.Metrics.OpenPreTriggerRate = openStats.Mean
		result.Metrics.OpenPreTriggerCV = openStats.CV
		if openStats.CV > cfg.Validity.MaximumPretriggerRateCV {
			result.InvalidReasons = append(result.InvalidReasons,
				fmt.Sprintf("open pre-trigger CV %.4f exceeds maximum %.4f",
					openStats.CV,
					cfg.Validity.MaximumPretriggerRateCV,
				),
			)
		}
	}

	if closedStats.Mean > 0 && openStats.Mean > 0 {
		difference := relativeDifference(closedStats.Mean, openStats.Mean)
		if difference > cfg.Validity.PretriggerRateToleranceRatio {
			result.InvalidReasons = append(result.InvalidReasons,
				fmt.Sprintf("pre-trigger rates differ by %.4f, above tolerance %.4f",
					difference,
					cfg.Validity.PretriggerRateToleranceRatio,
				),
			)
		}
	}

	closedPeak, err := peakInflight(closed)
	if err != nil {
		result.InvalidReasons = append(result.InvalidReasons, "closed in-flight telemetry: "+err.Error())
	} else {
		result.Metrics.ClosedPeakInflight = closedPeak
	}

	openPeak, err := peakInflight(opened)
	if err != nil {
		result.InvalidReasons = append(result.InvalidReasons, "open in-flight telemetry: "+err.Error())
	} else {
		result.Metrics.OpenPeakInflight = openPeak
	}

	if result.Metrics.ClosedPeakInflight > 0 {
		result.Metrics.PeakInflightRatio = result.Metrics.OpenPeakInflight / result.Metrics.ClosedPeakInflight
	}
	if result.Metrics.ClosedP99MS > 0 {
		result.Metrics.P99Ratio = result.Metrics.OpenP99MS / result.Metrics.ClosedP99MS
	}

	if closedStats.Mean > 0 {
		recovered, recoveredAt, err := findRecovery(
			closed.RateSamples,
			closed.Trigger.End,
			cfg.Recovery.StabilityWindow.Duration,
			closedStats.Mean,
			cfg.Validity.PretriggerRateToleranceRatio,
			cfg.Recovery.MaximumPosttriggerCV,
		)
		if err != nil {
			result.InvalidReasons = append(result.InvalidReasons, "closed recovery telemetry: "+err.Error())
		} else {
			result.Metrics.ClosedRecovered = recovered
			result.Metrics.ClosedRecoveryAt = recoveredAt
		}
	}

	if openStats.Mean > 0 {
		recovered, recoveredAt, err := findRecovery(
			opened.RateSamples,
			opened.Trigger.End,
			cfg.Recovery.StabilityWindow.Duration,
			openStats.Mean,
			cfg.Validity.PretriggerRateToleranceRatio,
			cfg.Recovery.MaximumPosttriggerCV,
		)
		if err != nil {
			result.InvalidReasons = append(result.InvalidReasons, "open recovery telemetry: "+err.Error())
		} else {
			result.Metrics.OpenRecovered = recovered
			result.Metrics.OpenRecoveryAt = recoveredAt
		}
	}

	if len(result.InvalidReasons) > 0 {
		result.Status = EvaluationInvalid
		return result
	}

	if result.Metrics.ClosedP99MS > cfg.Hypothesis.MaximumClosedP99MS {
		result.HypothesisReasons = append(result.HypothesisReasons,
			fmt.Sprintf("closed p99 %.2fms exceeds %.2fms",
				result.Metrics.ClosedP99MS,
				cfg.Hypothesis.MaximumClosedP99MS,
			),
		)
	}
	if result.Metrics.OpenP99MS < cfg.Hypothesis.MinimumOpenP99MS {
		result.HypothesisReasons = append(result.HypothesisReasons,
			fmt.Sprintf("open p99 %.2fms below %.2fms",
				result.Metrics.OpenP99MS,
				cfg.Hypothesis.MinimumOpenP99MS,
			),
		)
	}
	if result.Metrics.P99Ratio < cfg.Hypothesis.MinimumP99RatioOpenOverClosed {
		result.HypothesisReasons = append(result.HypothesisReasons,
			fmt.Sprintf("p99 ratio %.2f below %.2f",
				result.Metrics.P99Ratio,
				cfg.Hypothesis.MinimumP99RatioOpenOverClosed,
			),
		)
	}
	if result.Metrics.PeakInflightRatio < cfg.Hypothesis.MinimumPeakInflightRatioOpenOverClosed {
		result.HypothesisReasons = append(result.HypothesisReasons,
			fmt.Sprintf("peak in-flight ratio %.2f below %.2f",
				result.Metrics.PeakInflightRatio,
				cfg.Hypothesis.MinimumPeakInflightRatioOpenOverClosed,
			),
		)
	}

	if len(result.HypothesisReasons) > 0 {
		result.Status = EvaluationNotSupported
		return result
	}

	result.Status = EvaluationSupported
	return result
}

func validateTrialBasics(expectedScenario string, evidence TrialEvidence, result *PairEvaluation) {
	if evidence.K6.Scenario != expectedScenario {
		result.InvalidReasons = append(result.InvalidReasons,
			fmt.Sprintf("%s k6 scenario mismatch: %q", expectedScenario, evidence.K6.Scenario),
		)
	}
	if !isFinitePositive(evidence.K6.WorkLatencyP99MS) {
		result.InvalidReasons = append(result.InvalidReasons, expectedScenario+" p99 must be finite and positive")
	}
	if !isFiniteNonNegative(evidence.K6.IterationRate) {
		result.InvalidReasons = append(result.InvalidReasons, expectedScenario+" iteration rate must be finite and non-negative")
	}
	if !isFiniteNonNegative(evidence.K6.DroppedIterations) {
		result.InvalidReasons = append(result.InvalidReasons, expectedScenario+" dropped iterations must be finite and non-negative")
	}
	if evidence.Trigger.Generation == 0 ||
		evidence.Trigger.Start.IsZero() ||
		evidence.Trigger.End.IsZero() ||
		!evidence.Trigger.End.After(evidence.Trigger.Start) {
		result.InvalidReasons = append(result.InvalidReasons, expectedScenario+" trigger window is incomplete")
	}
}

func preTriggerStats(evidence TrialEvidence) (windowStats, error) {
	start := evidence.Trigger.Start.Add(-preTriggerWindow)
	return statsForWindow(evidence.RateSamples, start, evidence.Trigger.Start)
}

func statsForWindow(samples []MetricPoint, start, end time.Time) (windowStats, error) {
	if !end.After(start) {
		return windowStats{}, fmt.Errorf("window end must be after start")
	}

	selected := sortedWindow(samples, start, end, false)
	if len(selected) < 3 {
		return windowStats{}, fmt.Errorf("insufficient samples")
	}
	if selected[0].At.Sub(start) > rateEdgeTolerance {
		return windowStats{}, fmt.Errorf("telemetry starts too late")
	}
	if end.Sub(selected[len(selected)-1].At) > rateEdgeTolerance {
		return windowStats{}, fmt.Errorf("telemetry ends too early")
	}

	for i := 1; i < len(selected); i++ {
		if selected[i].At.Sub(selected[i-1].At) > maxRateSampleGap {
			return windowStats{}, fmt.Errorf("material telemetry gap")
		}
	}

	var sum float64
	for _, sample := range selected {
		if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
			return windowStats{}, fmt.Errorf("sample values must be finite and non-negative")
		}
		sum += sample.Value
	}

	mean := sum / float64(len(selected))
	if mean <= 0 {
		return windowStats{}, fmt.Errorf("window mean must be positive")
	}

	var squared float64
	for _, sample := range selected {
		delta := sample.Value - mean
		squared += delta * delta
	}
	stddev := math.Sqrt(squared / float64(len(selected)))

	return windowStats{Mean: mean, CV: stddev / mean}, nil
}

func peakInflight(evidence TrialEvidence) (float64, error) {
	selected := sortedWindow(evidence.InflightSamples, evidence.Trigger.Start, evidence.Trigger.End, true)
	if len(selected) < 2 {
		return 0, fmt.Errorf("insufficient samples across stall window")
	}

	stallDuration := evidence.Trigger.End.Sub(evidence.Trigger.Start)
	edgeTolerance := stallDuration / 2
	if edgeTolerance <= 0 {
		return 0, fmt.Errorf("invalid stall duration")
	}
	if selected[0].At.Sub(evidence.Trigger.Start) > edgeTolerance {
		return 0, fmt.Errorf("in-flight telemetry starts too late")
	}
	if evidence.Trigger.End.Sub(selected[len(selected)-1].At) > edgeTolerance {
		return 0, fmt.Errorf("in-flight telemetry ends too early")
	}
	for i := 1; i < len(selected); i++ {
		if selected[i].At.Sub(selected[i-1].At) > stallDuration {
			return 0, fmt.Errorf("material in-flight telemetry gap")
		}
	}

	peak := -1.0
	for _, sample := range selected {
		if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
			return 0, fmt.Errorf("in-flight samples must be finite and non-negative")
		}
		if sample.Value > peak {
			peak = sample.Value
		}
	}
	if peak <= 0 {
		return 0, fmt.Errorf("peak in-flight must be positive")
	}

	return peak, nil
}

func findRecovery(
	samples []MetricPoint,
	after time.Time,
	stabilityWindow time.Duration,
	baselineMean float64,
	rateTolerance float64,
	maxCV float64,
) (bool, *time.Time, error) {
	if stabilityWindow <= 0 {
		return false, nil, fmt.Errorf("stability window must be positive")
	}

	sorted := append([]MetricPoint(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })

	validWindowSeen := false
	for _, sample := range sorted {
		if sample.At.Before(after) {
			continue
		}
		windowEnd := sample.At.Add(stabilityWindow)
		stats, err := statsForWindow(sorted, sample.At, windowEnd)
		if err != nil {
			continue
		}
		validWindowSeen = true

		if stats.CV <= maxCV &&
			relativeDifference(stats.Mean, baselineMean) <= rateTolerance {
			recoveredAt := windowEnd
			return true, &recoveredAt, nil
		}
	}

	if !validWindowSeen {
		return false, nil, fmt.Errorf("insufficient post-trigger telemetry for stability window")
	}

	return false, nil, nil
}

func sortedWindow(samples []MetricPoint, start, end time.Time, includeEnd bool) []MetricPoint {
	selected := make([]MetricPoint, 0, len(samples))
	for _, sample := range samples {
		if sample.At.Before(start) {
			continue
		}
		if includeEnd {
			if sample.At.After(end) {
				continue
			}
		} else if !sample.At.Before(end) {
			continue
		}
		selected = append(selected, sample)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].At.Before(selected[j].At) })
	return selected
}

func relativeDifference(a, b float64) float64 {
	denominator := math.Max(math.Abs(a), math.Abs(b))
	if denominator == 0 {
		return 0
	}
	return math.Abs(a-b) / denominator
}
