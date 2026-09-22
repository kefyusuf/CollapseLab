package cf001

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `schema_version: 1
id: CF-001
name: coordinated-omission
version: 1
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
validity:
  pretrigger_rate_tolerance_ratio: 0.10
  max_dropped_iterations: 0
  minimum_open_achieved_ratio: 0.99
  maximum_pretrigger_rate_cv: 0.15
hypothesis:
  minimum_p99_ratio_open_over_closed: 5.0
  minimum_peak_inflight_ratio_open_over_closed: 5.0
  maximum_closed_p99_ms: 150
  minimum_open_p99_ms: 250
recovery:
  stability_window: 5s
  maximum_posttrigger_rate_cv: 0.15
`

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	path := writeTempConfig(t, validYAML+"\nunknown_field: true\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "field unknown_field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidDuration(t *testing.T) {
	path := writeTempConfig(t, strings.Replace(validYAML, "base_service_time: 50ms", "base_service_time: soon", 1))
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("expected duration parse error, got %v", err)
	}
}

func TestConfigValidateRejectsOpenVUCapacityBelowExpectedStallDemand(t *testing.T) {
	cfg := validConfig()
	cfg.Open.PreallocatedVUs = 10
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected insufficient open VU capacity to be rejected")
	}
}

func TestConfigValidateRequiresFiveSecondPreTriggerWindow(t *testing.T) {
	cfg := validConfig()
	cfg.Trial.TriggerAfter = duration(4 * time.Second)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected trigger window validation error")
	}
}

func TestConfigValidateRejectsInvalidProfileBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"schema version", func(c *Config) { c.SchemaVersion = 2 }},
		{"experiment id", func(c *Config) { c.ID = "CF-999" }},
		{"nonpositive version", func(c *Config) { c.Version = 0 }},
		{"nonpositive base service time", func(c *Config) { c.SUT.BaseServiceTime = duration(0) }},
		{"nonlocal work path", func(c *Config) { c.SUT.WorkPath = "https://example.test/work" }},
		{"short trial", func(c *Config) { c.Trial.Duration = duration(14 * time.Second) }},
		{"nonpositive stall", func(c *Config) { c.Trial.StallDuration = duration(0) }},
		{"insufficient recovery room", func(c *Config) { c.Trial.Duration = duration(15 * time.Second) }},
		{"closed executor", func(c *Config) { c.Closed.Executor = "shared-iterations" }},
		{"closed vus", func(c *Config) { c.Closed.VUs = 0 }},
		{"open executor", func(c *Config) { c.Open.Executor = "constant-vus" }},
		{"open rate", func(c *Config) { c.Open.RateRPS = 0 }},
		{"negative rate tolerance", func(c *Config) { c.Validity.PretriggerRateToleranceRatio = -0.01 }},
		{"rate tolerance at one", func(c *Config) { c.Validity.PretriggerRateToleranceRatio = 1 }},
		{"negative dropped iterations", func(c *Config) { c.Validity.MaxDroppedIterations = -1 }},
		{"zero achieved ratio", func(c *Config) { c.Validity.MinimumOpenAchievedRatio = 0 }},
		{"achieved ratio above one", func(c *Config) { c.Validity.MinimumOpenAchievedRatio = 1.01 }},
		{"negative pretrigger cv", func(c *Config) { c.Validity.MaximumPretriggerRateCV = -0.01 }},
		{"pretrigger cv at one", func(c *Config) { c.Validity.MaximumPretriggerRateCV = 1 }},
		{"p99 ratio at one", func(c *Config) { c.Hypothesis.MinimumP99RatioOpenOverClosed = 1 }},
		{"inflight ratio at one", func(c *Config) { c.Hypothesis.MinimumPeakInflightRatioOpenOverClosed = 1 }},
		{"nonpositive closed p99 ceiling", func(c *Config) { c.Hypothesis.MaximumClosedP99MS = 0 }},
		{"nonpositive open p99 floor", func(c *Config) { c.Hypothesis.MinimumOpenP99MS = 0 }},
		{"nonpositive stability window", func(c *Config) { c.Recovery.StabilityWindow = duration(0) }},
		{"negative posttrigger cv", func(c *Config) { c.Recovery.MaximumPosttriggerCV = -0.01 }},
		{"posttrigger cv at one", func(c *Config) { c.Recovery.MaximumPosttriggerCV = 1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", tt.name)
			}
		})
	}
}

func TestConfigValidateAcceptsCanonicalProfile(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected canonical profile to validate: %v", err)
	}
}

func validConfig() Config {
	return Config{
		SchemaVersion: 1,
		ID:            "CF-001",
		Name:          "coordinated-omission",
		Version:       1,
		SUT: SUTConfig{
			BaseServiceTime: duration(50 * time.Millisecond),
			WorkPath:        "/work",
		},
		Trial: TrialConfig{
			Duration:      duration(30 * time.Second),
			TriggerAfter:  duration(10 * time.Second),
			StallDuration: duration(500 * time.Millisecond),
		},
		Closed: ClosedConfig{Executor: "constant-vus", VUs: 5},
		Open: OpenConfig{
			Executor:        "constant-arrival-rate",
			RateRPS:         100,
			PreallocatedVUs: 100,
		},
		Validity: ValidityConfig{
			PretriggerRateToleranceRatio: 0.10,
			MaxDroppedIterations:         0,
			MinimumOpenAchievedRatio:     0.99,
			MaximumPretriggerRateCV:      0.15,
		},
		Hypothesis: HypothesisConfig{
			MinimumP99RatioOpenOverClosed:          5.0,
			MinimumPeakInflightRatioOpenOverClosed: 5.0,
			MaximumClosedP99MS:                     150,
			MinimumOpenP99MS:                       250,
		},
		Recovery: RecoveryConfig{
			StabilityWindow:      duration(5 * time.Second),
			MaximumPosttriggerCV: 0.15,
		},
	}
}

func duration(v time.Duration) Duration {
	return Duration{Duration: v}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestCanonicalConfigLoads(t *testing.T) {
	path := filepath.Join("..", "..", "experiments", "cf-001-coordinated-omission", "experiment.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load canonical config: %v", err)
	}
	if cfg != validConfig() {
		t.Fatalf("canonical config drifted:\n got: %#v\nwant: %#v", cfg, validConfig())
	}
}
