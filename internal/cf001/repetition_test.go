package cf001

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssessRepetitionsRequiresAtLeastThreeRuns(t *testing.T) {
	cfg := validConfig()

	assessment := AssessRepetitions(cfg, []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 4.8, 7.2, 99, 100, true, true),
	})

	if assessment.Passed {
		t.Fatalf("gate passed with fewer than three runs: %+v", assessment)
	}
	if !containsReason(assessment.Reasons, "at least 3") {
		t.Fatalf("missing repetition-count reason: %+v", assessment.Reasons)
	}
}

func TestAssessRepetitionsPassesRepeatedMechanismWhenAllRunsAreNotSupported(t *testing.T) {
	cfg := validConfig()

	assessment := AssessRepetitions(cfg, []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.70, 6.8, 98.0, 100.0, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 4.82, 7.4, 98.5, 100.1, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.76, 7.0, 97.8, 99.9, true, true),
	})

	if !assessment.Passed {
		t.Fatalf("repeated qualitative mechanism must pass gate: %+v", assessment)
	}
	if assessment.HypothesisConsensus != HypothesisAllNotSupported {
		t.Fatalf("consensus = %q", assessment.HypothesisConsensus)
	}
	if assessment.Stats.P99Ratio.Min >= assessment.Stats.P99Ratio.Max {
		t.Fatalf("p99 ratio stats did not preserve variation: %+v", assessment.Stats.P99Ratio)
	}
}

func TestAssessRepetitionsReportsMixedHypothesisOutcomesWithoutFailingMechanismGate(t *testing.T) {
	cfg := validConfig()

	assessment := AssessRepetitions(cfg, []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.98, 7.1, 98.0, 100.0, true, true),
		repetitionRun("run-2", EvaluationSupported, 5.04, 7.3, 98.4, 100.1, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.96, 7.0, 98.2, 99.9, true, true),
	})

	if !assessment.Passed {
		t.Fatalf("threshold crossing alone must not fail repetition gate: %+v", assessment)
	}
	if assessment.HypothesisConsensus != HypothesisMixed {
		t.Fatalf("consensus = %q, want MIXED", assessment.HypothesisConsensus)
	}
}

func TestAssessRepetitionsFailsWhenAnyRunIsInvalid(t *testing.T) {
	cfg := validConfig()
	runs := []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationInvalid, 4.8, 7.0, 98, 100, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
	}
	runs[1].InvalidReasons = []string{"generator saturation"}

	assessment := AssessRepetitions(cfg, runs)

	if assessment.Passed {
		t.Fatalf("invalid run must fail repetition gate: %+v", assessment)
	}
	if !containsReason(assessment.Reasons, "INVALID") {
		t.Fatalf("missing invalid-run reason: %+v", assessment.Reasons)
	}
}

func TestAssessRepetitionsFailsWhenQualitativeMechanismChangesDirection(t *testing.T) {
	cfg := validConfig()

	runs := []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 0.95, 7.0, 98, 100, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.8, 7.0, 98, 100, true, true),
	}

	assessment := AssessRepetitions(cfg, runs)

	if assessment.Passed {
		t.Fatalf("p99 direction reversal must fail repetition gate: %+v", assessment)
	}
	if !containsReason(assessment.Reasons, "p99 amplification") {
		t.Fatalf("missing mechanism-direction reason: %+v", assessment.Reasons)
	}
}

func TestAssessRepetitionsFailsWhenCrossRunBaselinesDriftBeyondTolerance(t *testing.T) {
	cfg := validConfig()

	assessment := AssessRepetitions(cfg, []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 4.8, 7.0, 99, 100, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.7, 7.0, 75, 100, true, true),
	})

	if assessment.Passed {
		t.Fatalf("cross-run baseline drift must fail repetition gate: %+v", assessment)
	}
	if !containsReason(assessment.Reasons, "closed pre-trigger") {
		t.Fatalf("missing baseline-drift reason: %+v", assessment.Reasons)
	}
}

func TestAssessRepetitionsFailsWhenRecoveryBehaviorIsNotStable(t *testing.T) {
	cfg := validConfig()

	assessment := AssessRepetitions(cfg, []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 4.8, 7.0, 98, 100, true, false),
		repetitionRun("run-3", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
	})

	if assessment.Passed {
		t.Fatalf("inconsistent recovery must fail repetition gate: %+v", assessment)
	}
	if !containsReason(assessment.Reasons, "recovery") {
		t.Fatalf("missing recovery reason: %+v", assessment.Reasons)
	}
}

func TestAssessRepetitionsFailsWhenRevisionConfigOrEnvironmentDiffers(t *testing.T) {
	cfg := validConfig()
	base := []RepetitionRun{
		repetitionRun("run-1", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
		repetitionRun("run-2", EvaluationNotSupported, 4.8, 7.0, 98, 100, true, true),
		repetitionRun("run-3", EvaluationNotSupported, 4.7, 7.0, 98, 100, true, true),
	}

	tests := []struct {
		name   string
		mutate func([]RepetitionRun)
		needle string
	}{
		{
			name: "revision",
			mutate: func(runs []RepetitionRun) { runs[2].RevisionSHA = strings.Repeat("d", 40) },
			needle: "revision",
		},
		{
			name: "config",
			mutate: func(runs []RepetitionRun) { runs[2].ConfigSHA256 = strings.Repeat("e", 64) },
			needle: "config",
		},
		{
			name: "environment",
			mutate: func(runs []RepetitionRun) { runs[2].EnvironmentSHA256 = strings.Repeat("f", 64) },
			needle: "environment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := append([]RepetitionRun(nil), base...)
			tt.mutate(runs)
			assessment := AssessRepetitions(cfg, runs)
			if assessment.Passed {
				t.Fatalf("%s mismatch must fail gate", tt.name)
			}
			if !containsReason(assessment.Reasons, tt.needle) {
				t.Fatalf("missing %s reason: %+v", tt.needle, assessment.Reasons)
			}
		})
	}
}

func TestRunCF001RepetitionsProducesThreeVerifiedBundlesAndCuratedSummary(t *testing.T) {
	configPath := writeTempConfig(t, validYAML)
	runsRoot := t.TempDir()
	cfg := validConfig()
	closed, opened := validEvidencePair(cfg)
	lab := &fakeCF001Lab{trials: map[string]CollectedTrial{
		"closed": collectedTrialFixture(t, closed),
		"open":   collectedTrialFixture(t, opened),
	}}

	now := time.Date(2026, 9, 23, 6, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		current := now
		now = now.Add(time.Second)
		return current
	}
	revision := RevisionEvidence{
		Commit:     strings.Repeat("a", 40),
		Branch:     "feat/cf-001-coordinated-omission",
		Repository: "https://github.com/kefyusuf/CollapseLab",
		Dirty:      false,
	}

	result, err := RunCF001Repetitions(
		context.Background(),
		RepetitionOptions{
			Count:       3,
			ConfigPath:  configPath,
			RunsRoot:    runsRoot,
			SummaryPath: filepath.Join(runsRoot, "repetition-summary.json"),
			ReportPath:  filepath.Join(runsRoot, "repetition-report.md"),
			Now:         nowFn,
		},
		RunnerDependencies{
			Revision: fakeRevisionSource{revision: revision},
			Lab:      lab,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Assessment.Passed {
		t.Fatalf("repetition gate failed: %+v", result.Assessment)
	}
	if len(result.BundleDirs) != 3 {
		t.Fatalf("bundle count = %d", len(result.BundleDirs))
	}

	seen := map[string]bool{}
	for _, dir := range result.BundleDirs {
		if seen[dir] {
			t.Fatalf("duplicate bundle directory %q", dir)
		}
		seen[dir] = true
		if err := VerifyRunBundle(dir); err != nil {
			t.Fatalf("verify bundle %s: %v", dir, err)
		}
	}

	summaryData, err := os.ReadFile(result.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var summary RepetitionSummary
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.SchemaVersion != 1 || summary.RunCount != 3 || !summary.Assessment.Passed {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.RevisionSHA != revision.Commit {
		t.Fatalf("summary revision = %q", summary.RevisionSHA)
	}
	if err := VerifyRepetitionSummary(result.SummaryPath, runsRoot); err != nil {
		t.Fatalf("verify repetition summary: %v", err)
	}
	if _, err := os.Stat(result.ReportPath); err != nil {
		t.Fatalf("repetition report: %v", err)
	}
}

func repetitionRun(
	id string,
	status EvaluationStatus,
	p99Ratio float64,
	inflightRatio float64,
	closedRate float64,
	openRate float64,
	closedRecovered bool,
	openRecovered bool,
) RepetitionRun {
	return RepetitionRun{
		RunID:             id,
		RevisionSHA:       strings.Repeat("a", 40),
		ConfigSHA256:      strings.Repeat("b", 64),
		EnvironmentSHA256: strings.Repeat("c", 64),
		ManifestSHA256:    strings.Repeat("d", 64),
		Status:            status,
		Metrics: PairMetrics{
			ClosedP99MS:          52,
			OpenP99MS:            52 * p99Ratio,
			P99Ratio:             p99Ratio,
			ClosedPeakInflight:   5,
			OpenPeakInflight:     5 * inflightRatio,
			PeakInflightRatio:    inflightRatio,
			ClosedPreTriggerRate: closedRate,
			OpenPreTriggerRate:   openRate,
			OpenAchievedRatio:    0.999,
			ClosedRecovered:      closedRecovered,
			OpenRecovered:        openRecovered,
		},
	}
}

func containsReason(reasons []string, needle string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, needle) {
			return true
		}
	}
	return false
}
