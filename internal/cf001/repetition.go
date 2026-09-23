package cf001

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type HypothesisConsensus string

const (
	HypothesisAllSupported    HypothesisConsensus = "ALL_SUPPORTED"
	HypothesisAllNotSupported HypothesisConsensus = "ALL_NOT_SUPPORTED"
	HypothesisMixed           HypothesisConsensus = "MIXED"
	HypothesisInvalid         HypothesisConsensus = "INVALID"
)

type ScalarStats struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Mean float64 `json:"mean"`
	CV   float64 `json:"cv"`
}

type RepetitionStats struct {
	ClosedP99MS          ScalarStats `json:"closed_p99_ms"`
	OpenP99MS            ScalarStats `json:"open_p99_ms"`
	P99Ratio             ScalarStats `json:"p99_ratio"`
	PeakInflightRatio    ScalarStats `json:"peak_inflight_ratio"`
	ClosedPreTriggerRate ScalarStats `json:"closed_pretrigger_rate"`
	OpenPreTriggerRate   ScalarStats `json:"open_pretrigger_rate"`
	OpenAchievedRatio    ScalarStats `json:"open_achieved_ratio"`
}

type RepetitionRun struct {
	RunID             string           `json:"run_id"`
	RevisionSHA       string           `json:"revision_sha"`
	ConfigSHA256      string           `json:"config_sha256"`
	EnvironmentSHA256 string           `json:"environment_sha256"`
	ManifestSHA256    string           `json:"manifest_sha256"`
	Status            EvaluationStatus `json:"status"`
	InvalidReasons    []string         `json:"invalid_reasons,omitempty"`
	HypothesisReasons []string         `json:"hypothesis_reasons,omitempty"`
	Metrics           PairMetrics      `json:"metrics"`
}

type RepetitionAssessment struct {
	Passed              bool                `json:"passed"`
	Reasons             []string            `json:"reasons,omitempty"`
	HypothesisConsensus HypothesisConsensus `json:"hypothesis_consensus"`
	Stats               RepetitionStats     `json:"stats"`
}

type RepetitionSummary struct {
	SchemaVersion     int                  `json:"schema_version"`
	CreatedAt         time.Time            `json:"created_at"`
	RevisionSHA       string               `json:"revision_sha"`
	ConfigSHA256      string               `json:"config_sha256"`
	EnvironmentSHA256 string               `json:"environment_sha256"`
	RunCount          int                  `json:"run_count"`
	Runs              []RepetitionRun      `json:"runs"`
	Assessment        RepetitionAssessment `json:"assessment"`
}

type RepetitionOptions struct {
	Count       int
	ConfigPath  string
	RunsRoot    string
	SummaryPath string
	ReportPath  string
	Now         func() time.Time
}

type RepetitionResult struct {
	SummaryPath string
	ReportPath  string
	BundleDirs  []string
	Summary     RepetitionSummary
	Assessment  RepetitionAssessment
}

func AssessRepetitions(cfg Config, runs []RepetitionRun) RepetitionAssessment {
	assessment := RepetitionAssessment{
		HypothesisConsensus: repetitionConsensus(runs),
		Stats:               repetitionStats(runs),
	}

	if len(runs) < 3 {
		assessment.Reasons = append(assessment.Reasons,
			fmt.Sprintf("at least 3 canonical runs are required, got %d", len(runs)),
		)
	}

	if len(runs) == 0 {
		assessment.Passed = false
		return assessment
	}

	revision := runs[0].RevisionSHA
	configDigest := runs[0].ConfigSHA256
	environmentDigest := runs[0].EnvironmentSHA256

	for _, run := range runs {
		if strings.TrimSpace(run.RunID) == "" {
			assessment.Reasons = append(assessment.Reasons, "run id is required")
		}
		if run.RevisionSHA != revision {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s revision differs from repetition revision", run.RunID),
			)
		}
		if run.ConfigSHA256 != configDigest {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s config digest differs from repetition config", run.RunID),
			)
		}
		if run.EnvironmentSHA256 != environmentDigest {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s environment identity differs across repetitions", run.RunID),
			)
		}

		switch run.Status {
		case EvaluationInvalid:
			reason := fmt.Sprintf("run %s is INVALID", run.RunID)
			if len(run.InvalidReasons) > 0 {
				reason += ": " + strings.Join(run.InvalidReasons, "; ")
			}
			assessment.Reasons = append(assessment.Reasons, reason)
		case EvaluationSupported, EvaluationNotSupported:
		default:
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s has unknown evaluation status %q", run.RunID, run.Status),
			)
		}

		if !finiteGreaterThan(run.Metrics.P99Ratio, 1) {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s lacks p99 amplification: ratio %.6f", run.RunID, run.Metrics.P99Ratio),
			)
		}
		if !finiteGreaterThan(run.Metrics.PeakInflightRatio, 1) {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s lacks in-flight amplification: ratio %.6f", run.RunID, run.Metrics.PeakInflightRatio),
			)
		}
		if !run.Metrics.ClosedRecovered || !run.Metrics.OpenRecovered {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s recovery is not stable: closed=%t open=%t",
					run.RunID,
					run.Metrics.ClosedRecovered,
					run.Metrics.OpenRecovered,
				),
			)
		}
		if !isFiniteNonNegative(run.Metrics.OpenAchievedRatio) ||
			run.Metrics.OpenAchievedRatio < cfg.Validity.MinimumOpenAchievedRatio {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("run %s open achieved ratio %.6f indicates generator saturation",
					run.RunID,
					run.Metrics.OpenAchievedRatio,
				),
			)
		}
	}

	if len(runs) >= 2 {
		closedSpread := relativeSpread(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.ClosedPreTriggerRate
		}))
		if closedSpread > cfg.Validity.PretriggerRateToleranceRatio {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("closed pre-trigger cross-run spread %.6f exceeds tolerance %.6f",
					closedSpread,
					cfg.Validity.PretriggerRateToleranceRatio,
				),
			)
		}

		openSpread := relativeSpread(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.OpenPreTriggerRate
		}))
		if openSpread > cfg.Validity.PretriggerRateToleranceRatio {
			assessment.Reasons = append(assessment.Reasons,
				fmt.Sprintf("open pre-trigger cross-run spread %.6f exceeds tolerance %.6f",
					openSpread,
					cfg.Validity.PretriggerRateToleranceRatio,
				),
			)
		}
	}

	assessment.Passed = len(assessment.Reasons) == 0
	return assessment
}

func RunCF001Repetitions(
	ctx context.Context,
	opts RepetitionOptions,
	deps RunnerDependencies,
) (RepetitionResult, error) {
	if opts.Count < 3 {
		return RepetitionResult{}, fmt.Errorf("at least 3 repetitions are required, got %d", opts.Count)
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		return RepetitionResult{}, errors.New("config path is required")
	}
	if strings.TrimSpace(opts.RunsRoot) == "" {
		return RepetitionResult{}, errors.New("runs root is required")
	}
	if strings.TrimSpace(opts.SummaryPath) == "" {
		return RepetitionResult{}, errors.New("summary path is required")
	}
	if strings.TrimSpace(opts.ReportPath) == "" {
		return RepetitionResult{}, errors.New("report path is required")
	}
	if deps.Revision == nil || deps.Lab == nil {
		return RepetitionResult{}, errors.New("revision source and CF-001 lab are required")
	}

	configBytes, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return RepetitionResult{}, fmt.Errorf("read repetition config: %w", err)
	}
	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return RepetitionResult{}, err
	}
	configDigest := SHA256Hex(configBytes)

	revision, err := deps.Revision.Revision(ctx)
	if err != nil {
		return RepetitionResult{}, fmt.Errorf("read repetition revision: %w", err)
	}
	if revision.Dirty {
		return RepetitionResult{}, errors.New("dirty revision cannot produce canonical repetition evidence")
	}
	if strings.TrimSpace(revision.Commit) == "" {
		return RepetitionResult{}, errors.New("revision commit is required")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}

	runs := make([]RepetitionRun, 0, opts.Count)
	bundleDirs := make([]string, 0, opts.Count)

	for i := 0; i < opts.Count; i++ {
		result, err := RunCF001(
			ctx,
			RunnerOptions{
				ConfigPath: opts.ConfigPath,
				RunsRoot:   opts.RunsRoot,
				Now:        now,
			},
			deps,
		)
		if err != nil {
			return RepetitionResult{}, fmt.Errorf("run repetition %d: %w", i+1, err)
		}
		if err := VerifyRunBundle(result.BundleDir); err != nil {
			return RepetitionResult{}, fmt.Errorf("verify repetition %d bundle: %w", i+1, err)
		}

		run, err := repetitionRunFromBundle(result.BundleDir, result.Evaluation)
		if err != nil {
			return RepetitionResult{}, fmt.Errorf("read repetition %d evidence: %w", i+1, err)
		}
		runs = append(runs, run)
		bundleDirs = append(bundleDirs, result.BundleDir)
	}

	assessment := AssessRepetitions(cfg, runs)
	createdAt := now().UTC()
	environmentDigest := ""
	if len(runs) > 0 {
		environmentDigest = runs[0].EnvironmentSHA256
	}

	summary := RepetitionSummary{
		SchemaVersion:     1,
		CreatedAt:         createdAt,
		RevisionSHA:       revision.Commit,
		ConfigSHA256:      configDigest,
		EnvironmentSHA256: environmentDigest,
		RunCount:          len(runs),
		Runs:              runs,
		Assessment:        assessment,
	}

	if err := writeRepetitionJSON(opts.SummaryPath, summary); err != nil {
		return RepetitionResult{}, err
	}
	if err := writeRepetitionReport(opts.ReportPath, summary); err != nil {
		return RepetitionResult{}, err
	}
	if err := VerifyRepetitionSummary(opts.SummaryPath, opts.RunsRoot); err != nil {
		return RepetitionResult{}, fmt.Errorf("verify repetition summary: %w", err)
	}

	return RepetitionResult{
		SummaryPath: opts.SummaryPath,
		ReportPath:  opts.ReportPath,
		BundleDirs:  bundleDirs,
		Summary:     summary,
		Assessment:  assessment,
	}, nil
}

func VerifyRepetitionSummary(summaryPath, runsRoot string) error {
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return fmt.Errorf("read repetition summary: %w", err)
	}
	var summary RepetitionSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return fmt.Errorf("decode repetition summary: %w", err)
	}
	if summary.SchemaVersion != 1 {
		return fmt.Errorf("unsupported repetition schema_version %d", summary.SchemaVersion)
	}
	if summary.RunCount != len(summary.Runs) {
		return fmt.Errorf("repetition run count mismatch: declared=%d actual=%d", summary.RunCount, len(summary.Runs))
	}
	if summary.RunCount < 3 {
		return fmt.Errorf("repetition summary requires at least 3 runs, got %d", summary.RunCount)
	}
	if strings.TrimSpace(summary.RevisionSHA) == "" || !validSHA256(summary.ConfigSHA256) ||
		!validSHA256(summary.EnvironmentSHA256) {
		return errors.New("repetition summary identity is incomplete")
	}

	configPath := ""
	verifiedRuns := make([]RepetitionRun, 0, len(summary.Runs))

	for _, declared := range summary.Runs {
		dir := filepath.Join(runsRoot, declared.RunID)
		if err := VerifyRunBundle(dir); err != nil {
			return fmt.Errorf("verify child bundle %s: %w", declared.RunID, err)
		}

		manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			return err
		}
		var manifest BundleManifest
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			return err
		}
		if manifest.RevisionSHA != summary.RevisionSHA ||
			manifest.ConfigSHA256 != summary.ConfigSHA256 {
			return fmt.Errorf("child bundle %s identity differs from repetition summary", declared.RunID)
		}

		environmentBytes, err := os.ReadFile(filepath.Join(dir, "environment.json"))
		if err != nil {
			return err
		}
		environmentDigest := SHA256Hex(environmentBytes)

		assertionsBytes, err := os.ReadFile(filepath.Join(dir, "assertions.json"))
		if err != nil {
			return err
		}
		var assertions assertionsDocument
		if err := json.Unmarshal(assertionsBytes, &assertions); err != nil {
			return err
		}

		actual := RepetitionRun{
			RunID:             manifest.RunID,
			RevisionSHA:       manifest.RevisionSHA,
			ConfigSHA256:      manifest.ConfigSHA256,
			EnvironmentSHA256: environmentDigest,
			ManifestSHA256:    SHA256Hex(manifestBytes),
			Status:            assertions.Status,
			InvalidReasons:    assertions.Invalid,
			HypothesisReasons: assertions.Hypothesis,
			Metrics:           assertions.Metrics,
		}
		if !reflect.DeepEqual(declared, actual) {
			return fmt.Errorf("child bundle %s does not match curated repetition run", declared.RunID)
		}
		verifiedRuns = append(verifiedRuns, actual)

		if configPath == "" {
			configPath = filepath.Join(dir, "experiment.yaml")
		}
	}

	if len(verifiedRuns) > 0 && verifiedRuns[0].EnvironmentSHA256 != summary.EnvironmentSHA256 {
		return errors.New("repetition environment digest mismatch")
	}

	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read repetition config: %w", err)
	}
	cfg, err := decodeConfigBytes(cfgBytes)
	if err != nil {
		return err
	}
	recomputed := AssessRepetitions(cfg, verifiedRuns)
	if !reflect.DeepEqual(summary.Assessment, recomputed) {
		return errors.New("repetition assessment does not match child bundle evidence")
	}

	return nil
}

func repetitionRunFromBundle(dir string, evaluation PairEvaluation) (RepetitionRun, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return RepetitionRun{}, err
	}
	var manifest BundleManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return RepetitionRun{}, err
	}

	environmentBytes, err := os.ReadFile(filepath.Join(dir, "environment.json"))
	if err != nil {
		return RepetitionRun{}, err
	}

	return RepetitionRun{
		RunID:             manifest.RunID,
		RevisionSHA:       manifest.RevisionSHA,
		ConfigSHA256:      manifest.ConfigSHA256,
		EnvironmentSHA256: SHA256Hex(environmentBytes),
		ManifestSHA256:    SHA256Hex(manifestBytes),
		Status:            evaluation.Status,
		InvalidReasons:    append([]string(nil), evaluation.InvalidReasons...),
		HypothesisReasons: append([]string(nil), evaluation.HypothesisReasons...),
		Metrics:           evaluation.Metrics,
	}, nil
}

func decodeConfigBytes(data []byte) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode repetition config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate repetition config: %w", err)
	}
	return cfg, nil
}

func writeRepetitionJSON(path string, summary RepetitionSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create repetition summary directory: %w", err)
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("encode repetition summary: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write repetition summary: %w", err)
	}
	return nil
}

func writeRepetitionReport(path string, summary RepetitionSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create repetition report directory: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# CF-001 Canonical Repetition Report\n\n")
	fmt.Fprintf(&b, "- Revision: `%s`\n", summary.RevisionSHA)
	fmt.Fprintf(&b, "- Run count: %d\n", summary.RunCount)
	fmt.Fprintf(&b, "- Repetition gate: **%s**\n", passLabel(summary.Assessment.Passed))
	fmt.Fprintf(&b, "- Hypothesis consensus: **%s**\n\n", summary.Assessment.HypothesisConsensus)

	b.WriteString("| Run | Evaluation | Closed p99 ms | Open p99 ms | p99 ratio | In-flight ratio | Open achieved | Recovery |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |\n")
	for _, run := range summary.Runs {
		fmt.Fprintf(
			&b,
			"| %s | %s | %.6f | %.6f | %.6f | %.6f | %.6f | %t/%t |\n",
			run.RunID,
			run.Status,
			run.Metrics.ClosedP99MS,
			run.Metrics.OpenP99MS,
			run.Metrics.P99Ratio,
			run.Metrics.PeakInflightRatio,
			run.Metrics.OpenAchievedRatio,
			run.Metrics.ClosedRecovered,
			run.Metrics.OpenRecovered,
		)
	}

	b.WriteString("\n## Cross-run statistics\n\n")
	fmt.Fprintf(&b, "- p99 ratio min/mean/max: %.6f / %.6f / %.6f\n",
		summary.Assessment.Stats.P99Ratio.Min,
		summary.Assessment.Stats.P99Ratio.Mean,
		summary.Assessment.Stats.P99Ratio.Max,
	)
	fmt.Fprintf(&b, "- peak in-flight ratio min/mean/max: %.6f / %.6f / %.6f\n",
		summary.Assessment.Stats.PeakInflightRatio.Min,
		summary.Assessment.Stats.PeakInflightRatio.Mean,
		summary.Assessment.Stats.PeakInflightRatio.Max,
	)
	fmt.Fprintf(&b, "- open p99 min/mean/max: %.6f / %.6f / %.6f ms\n",
		summary.Assessment.Stats.OpenP99MS.Min,
		summary.Assessment.Stats.OpenP99MS.Mean,
		summary.Assessment.Stats.OpenP99MS.Max,
	)

	if len(summary.Assessment.Reasons) > 0 {
		b.WriteString("\n## Repetition gate reasons\n\n")
		for _, reason := range summary.Assessment.Reasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write repetition report: %w", err)
	}
	return nil
}

func repetitionConsensus(runs []RepetitionRun) HypothesisConsensus {
	if len(runs) == 0 {
		return HypothesisInvalid
	}
	supported := 0
	notSupported := 0
	for _, run := range runs {
		switch run.Status {
		case EvaluationSupported:
			supported++
		case EvaluationNotSupported:
			notSupported++
		default:
			return HypothesisInvalid
		}
	}
	switch {
	case supported == len(runs):
		return HypothesisAllSupported
	case notSupported == len(runs):
		return HypothesisAllNotSupported
	default:
		return HypothesisMixed
	}
}

func repetitionStats(runs []RepetitionRun) RepetitionStats {
	return RepetitionStats{
		ClosedP99MS: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.ClosedP99MS
		})),
		OpenP99MS: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.OpenP99MS
		})),
		P99Ratio: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.P99Ratio
		})),
		PeakInflightRatio: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.PeakInflightRatio
		})),
		ClosedPreTriggerRate: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.ClosedPreTriggerRate
		})),
		OpenPreTriggerRate: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.OpenPreTriggerRate
		})),
		OpenAchievedRatio: scalarStats(extract(runs, func(run RepetitionRun) float64 {
			return run.Metrics.OpenAchievedRatio
		})),
	}
}

func extract(runs []RepetitionRun, getter func(RepetitionRun) float64) []float64 {
	values := make([]float64, 0, len(runs))
	for _, run := range runs {
		values = append(values, getter(run))
	}
	return values
}

func scalarStats(values []float64) ScalarStats {
	if len(values) == 0 {
		return ScalarStats{}
	}
	minimum := values[0]
	maximum := values[0]
	var sum float64
	for _, value := range values {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
		sum += value
	}
	mean := sum / float64(len(values))
	var squared float64
	for _, value := range values {
		delta := value - mean
		squared += delta * delta
	}
	cv := 0.0
	if mean != 0 {
		cv = math.Sqrt(squared/float64(len(values))) / math.Abs(mean)
	}
	return ScalarStats{Min: minimum, Max: maximum, Mean: mean, CV: cv}
}

func relativeSpread(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	stats := scalarStats(values)
	if stats.Max == 0 {
		if stats.Min == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return math.Abs(stats.Max-stats.Min) / math.Abs(stats.Max)
}

func finiteGreaterThan(value, threshold float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > threshold
}

func passLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}
