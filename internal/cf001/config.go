package cf001

import (
	"fmt"
	"math"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	canonicalExperimentName    = "coordinated-omission"
	canonicalExperimentVersion = 1
	canonicalBaseServiceTime   = 50 * time.Millisecond
	canonicalWorkPath          = "/work"
	canonicalTrialDuration     = 30 * time.Second
	canonicalClosedVUs         = 5
	canonicalOpenRateRPS       = 100
	canonicalOpenVUs           = 100
	maxControlAfter            = 25 * time.Second
	maxControlStallDuration    = 5 * time.Second
)

// Duration keeps experiment configuration human-readable while preserving
// time.Duration semantics inside the implementation.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	value, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", node.Value, err)
	}
	d.Duration = value
	return nil
}

type SUTConfig struct {
	BaseServiceTime Duration `yaml:"base_service_time"`
	WorkPath        string   `yaml:"work_path"`
}

type TrialConfig struct {
	Duration      Duration `yaml:"duration"`
	TriggerAfter  Duration `yaml:"trigger_after"`
	StallDuration Duration `yaml:"stall_duration"`
}

type ClosedConfig struct {
	Executor string `yaml:"executor"`
	VUs      int    `yaml:"vus"`
}

type OpenConfig struct {
	Executor        string `yaml:"executor"`
	RateRPS         int    `yaml:"rate_rps"`
	PreallocatedVUs int    `yaml:"preallocated_vus"`
}

type ValidityConfig struct {
	PretriggerRateToleranceRatio float64 `yaml:"pretrigger_rate_tolerance_ratio"`
	MaxDroppedIterations         int64   `yaml:"max_dropped_iterations"`
	MinimumOpenAchievedRatio     float64 `yaml:"minimum_open_achieved_ratio"`
	MaximumPretriggerRateCV      float64 `yaml:"maximum_pretrigger_rate_cv"`
}

type HypothesisConfig struct {
	MinimumP99RatioOpenOverClosed          float64 `yaml:"minimum_p99_ratio_open_over_closed"`
	MinimumPeakInflightRatioOpenOverClosed float64 `yaml:"minimum_peak_inflight_ratio_open_over_closed"`
	MaximumClosedP99MS                     float64 `yaml:"maximum_closed_p99_ms"`
	MinimumOpenP99MS                       float64 `yaml:"minimum_open_p99_ms"`
}

type RecoveryConfig struct {
	StabilityWindow      Duration `yaml:"stability_window"`
	MaximumPosttriggerCV float64  `yaml:"maximum_posttrigger_rate_cv"`
}

type Config struct {
	SchemaVersion int              `yaml:"schema_version"`
	ID            string           `yaml:"id"`
	Name          string           `yaml:"name"`
	Version       int              `yaml:"version"`
	SUT           SUTConfig        `yaml:"sut"`
	Trial         TrialConfig      `yaml:"trial"`
	Closed        ClosedConfig     `yaml:"closed"`
	Open          OpenConfig       `yaml:"open"`
	Validity      ValidityConfig   `yaml:"validity"`
	Hypothesis    HypothesisConfig `yaml:"hypothesis"`
	Recovery      RecoveryConfig   `yaml:"recovery"`
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if cfg.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if cfg.ID != "CF-001" {
		return fmt.Errorf("id must be CF-001")
	}
	if cfg.Name != canonicalExperimentName {
		return fmt.Errorf("name must be %q for CF-001 v1", canonicalExperimentName)
	}
	if cfg.Version != canonicalExperimentVersion {
		return fmt.Errorf("version must be %d for CF-001 v1", canonicalExperimentVersion)
	}
	if cfg.SUT.BaseServiceTime.Duration != canonicalBaseServiceTime {
		return fmt.Errorf("sut.base_service_time must be %s for CF-001 v1", canonicalBaseServiceTime)
	}
	if cfg.SUT.WorkPath != canonicalWorkPath {
		return fmt.Errorf("sut.work_path must be %q for CF-001 v1", canonicalWorkPath)
	}
	if cfg.Trial.Duration.Duration != canonicalTrialDuration {
		return fmt.Errorf("trial.duration must be %s for CF-001 v1", canonicalTrialDuration)
	}
	if cfg.Trial.TriggerAfter.Duration < 5*time.Second {
		return fmt.Errorf("trial.trigger_after must be at least 5s")
	}
	if cfg.Trial.TriggerAfter.Duration > maxControlAfter {
		return fmt.Errorf("trial.trigger_after must not exceed %s", maxControlAfter)
	}
	if cfg.Trial.StallDuration.Duration <= 0 {
		return fmt.Errorf("trial.stall_duration must be positive")
	}
	if cfg.Trial.StallDuration.Duration > maxControlStallDuration {
		return fmt.Errorf("trial.stall_duration must not exceed %s", maxControlStallDuration)
	}
	if cfg.Recovery.StabilityWindow.Duration <= 0 {
		return fmt.Errorf("recovery.stability_window must be positive")
	}
	occupied := cfg.Trial.TriggerAfter.Duration + cfg.Trial.StallDuration.Duration + cfg.Recovery.StabilityWindow.Duration
	if occupied >= cfg.Trial.Duration.Duration {
		return fmt.Errorf("trial must leave time after trigger, stall, and recovery stability window")
	}
	if cfg.Closed.Executor != "constant-vus" {
		return fmt.Errorf("closed.executor must be constant-vus")
	}
	if cfg.Closed.VUs != canonicalClosedVUs {
		return fmt.Errorf("closed.vus must be %d for CF-001 v1", canonicalClosedVUs)
	}
	if cfg.Open.Executor != "constant-arrival-rate" {
		return fmt.Errorf("open.executor must be constant-arrival-rate")
	}
	if cfg.Open.RateRPS != canonicalOpenRateRPS {
		return fmt.Errorf("open.rate_rps must be %d for CF-001 v1", canonicalOpenRateRPS)
	}
	if cfg.Open.PreallocatedVUs != canonicalOpenVUs {
		return fmt.Errorf("open.preallocated_vus must be %d for CF-001 v1", canonicalOpenVUs)
	}
	minimumOpenVUs := int(math.Ceil(float64(cfg.Open.RateRPS)*cfg.Trial.StallDuration.Duration.Seconds())) + 10
	if cfg.Open.PreallocatedVUs < minimumOpenVUs {
		return fmt.Errorf("open.preallocated_vus must be at least %d for the configured stall demand", minimumOpenVUs)
	}
	if err := validateUnitIntervalExclusiveOne("validity.pretrigger_rate_tolerance_ratio", cfg.Validity.PretriggerRateToleranceRatio); err != nil {
		return err
	}
	if cfg.Validity.MaxDroppedIterations < 0 {
		return fmt.Errorf("validity.max_dropped_iterations must not be negative")
	}
	if cfg.Validity.MinimumOpenAchievedRatio <= 0 || cfg.Validity.MinimumOpenAchievedRatio > 1 {
		return fmt.Errorf("validity.minimum_open_achieved_ratio must be in (0, 1]")
	}
	if err := validateUnitIntervalExclusiveOne("validity.maximum_pretrigger_rate_cv", cfg.Validity.MaximumPretriggerRateCV); err != nil {
		return err
	}
	if cfg.Hypothesis.MinimumP99RatioOpenOverClosed <= 1 {
		return fmt.Errorf("hypothesis.minimum_p99_ratio_open_over_closed must be greater than 1")
	}
	if cfg.Hypothesis.MinimumPeakInflightRatioOpenOverClosed <= 1 {
		return fmt.Errorf("hypothesis.minimum_peak_inflight_ratio_open_over_closed must be greater than 1")
	}
	if cfg.Hypothesis.MaximumClosedP99MS <= 0 {
		return fmt.Errorf("hypothesis.maximum_closed_p99_ms must be positive")
	}
	if cfg.Hypothesis.MinimumOpenP99MS <= 0 {
		return fmt.Errorf("hypothesis.minimum_open_p99_ms must be positive")
	}
	if err := validateUnitIntervalExclusiveOne("recovery.maximum_posttrigger_rate_cv", cfg.Recovery.MaximumPosttriggerCV); err != nil {
		return err
	}
	return nil
}

func validateUnitIntervalExclusiveOne(name string, value float64) error {
	if value < 0 || value >= 1 {
		return fmt.Errorf("%s must be in [0, 1)", name)
	}
	return nil
}
