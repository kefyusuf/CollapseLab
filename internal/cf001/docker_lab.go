package cf001

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	canonicalK6Image        = "grafana/k6:2.2.0"
	canonicalPrometheusImage = "prom/prometheus:v3.14.0"
	sutHealthURL             = "http://127.0.0.1:18080/healthz"
	sutControlBaseURL        = "http://127.0.0.1:19091"
	prometheusBaseURL        = "http://127.0.0.1:19090"
)

type DockerCF001Lab struct {
	RepoRoot   string
	HTTPClient *http.Client
}

func (l *DockerCF001Lab) Start(ctx context.Context) (EnvironmentEvidence, error) {
	root, err := l.repoRoot()
	if err != nil {
		return EnvironmentEvidence{}, err
	}

	if err := l.compose(ctx, "down", "--remove-orphans", "-v"); err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("clean lab state: %w", err)
	}
	if err := l.ensureImage(ctx, canonicalPrometheusImage); err != nil {
		return EnvironmentEvidence{}, err
	}
	if err := l.ensureImage(ctx, canonicalK6Image); err != nil {
		return EnvironmentEvidence{}, err
	}
	if err := l.compose(ctx, "up", "-d", "--build", "sut", "prometheus"); err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("start lab: %w", err)
	}

	if err := l.waitForStatus(ctx, sutHealthURL, http.StatusNoContent, 30*time.Second); err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("wait for SUT: %w", err)
	}
	if err := l.waitForStatus(ctx, prometheusBaseURL+"/-/ready", http.StatusOK, 30*time.Second); err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("wait for Prometheus: %w", err)
	}
	if err := l.waitForPrometheusTarget(ctx, 15*time.Second); err != nil {
		return EnvironmentEvidence{}, err
	}

	dockerVersion, err := l.commandOutput(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("docker version: %w", err)
	}
	composeVersion, err := l.commandOutput(ctx, "docker", "compose", "version", "--short")
	if err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("docker compose version: %w", err)
	}
	sutImage, err := l.commandOutput(ctx, "docker", "compose", "images", "-q", "sut")
	if err != nil {
		return EnvironmentEvidence{}, fmt.Errorf("resolve SUT image: %w", err)
	}
	k6Image, err := l.resolvedImageIdentity(ctx, canonicalK6Image)
	if err != nil {
		return EnvironmentEvidence{}, err
	}
	prometheusImage, err := l.resolvedImageIdentity(ctx, canonicalPrometheusImage)
	if err != nil {
		return EnvironmentEvidence{}, err
	}

	_ = root
	return EnvironmentEvidence{
		GoVersion:            runtime.Version(),
		DockerVersion:        dockerVersion,
		DockerComposeVersion: composeVersion,
		K6Image:              k6Image,
		PrometheusImage:      prometheusImage,
		SUTImage:             sutImage,
		GOOS:                 runtime.GOOS,
		GOARCH:               runtime.GOARCH,
		NumCPU:               runtime.NumCPU(),
	}, nil
}

func (l *DockerCF001Lab) Stop(ctx context.Context) error {
	if _, err := l.repoRoot(); err != nil {
		return err
	}
	if err := l.compose(ctx, "down", "--remove-orphans", "-v"); err != nil {
		return fmt.Errorf("stop lab: %w", err)
	}
	return nil
}

func (l *DockerCF001Lab) CollectTrial(
	ctx context.Context,
	scenario string,
	cfg Config,
	bundleDir string,
) (CollectedTrial, error) {
	if scenario != "closed" && scenario != "open" {
		return CollectedTrial{}, fmt.Errorf("unsupported scenario %q", scenario)
	}
	if err := l.requireIdleControlState(ctx); err != nil {
		return CollectedTrial{}, err
	}
	if err := l.waitForStatus(ctx, sutHealthURL, http.StatusNoContent, 10*time.Second); err != nil {
		return CollectedTrial{}, fmt.Errorf("SUT is not healthy before %s trial: %w", scenario, err)
	}

	containerSummaryPath, hostSummaryPath, err := l.summaryPaths(bundleDir, scenario)
	if err != nil {
		return CollectedTrial{}, err
	}
	if err := os.MkdirAll(filepath.Dir(hostSummaryPath), 0o755); err != nil {
		return CollectedTrial{}, fmt.Errorf("create scenario evidence directory: %w", err)
	}

	uid, gid, err := l.hostUIDGID(ctx)
	if err != nil {
		return CollectedTrial{}, err
	}
	workloadPath := "/workloads/cf001/" + scenario + ".js"
	k6 := exec.CommandContext(
		ctx,
		"docker", "compose", "--profile", "tools", "run", "--rm",
		"--user", uid+":"+gid,
		"-e", "SUMMARY_PATH="+containerSummaryPath,
		"k6", "run", workloadPath,
	)
	root, err := l.repoRoot()
	if err != nil {
		return CollectedTrial{}, err
	}
	k6.Dir = root
	k6.Stdout = os.Stderr
	k6.Stderr = os.Stderr

	workloadStarted := time.Now().UTC()
	if err := k6.Start(); err != nil {
		return CollectedTrial{}, fmt.Errorf("start %s k6 workload: %w", scenario, err)
	}

	killK6 := func() {
		if k6.Process != nil {
			_ = k6.Process.Kill()
			_, _ = k6.Process.Wait()
		}
	}

	if err := l.waitForScenarioTraffic(ctx, scenario, 10*time.Second); err != nil {
		killK6()
		return CollectedTrial{}, fmt.Errorf("wait for %s traffic: %w", scenario, err)
	}
	trafficDetectedAt := time.Now().UTC()

	scheduleRaw, err := l.scheduleStall(ctx, cfg.Trial.TriggerAfter.Duration, cfg.Trial.StallDuration.Duration)
	if err != nil {
		killK6()
		return CollectedTrial{}, err
	}
	scheduledAt := time.Now().UTC()

	if err := k6.Wait(); err != nil {
		return CollectedTrial{}, fmt.Errorf("%s k6 workload failed: %w", scenario, err)
	}
	workloadEnded := time.Now().UTC()

	triggerRaw, trigger, err := l.waitForCompletedTrigger(ctx, 5*time.Second)
	if err != nil {
		return CollectedTrial{}, err
	}

	rateEnd := trigger.End.Add(cfg.Recovery.StabilityWindow.Duration + 2*time.Second)
	rateRaw, err := l.queryRange(
		ctx,
		fmt.Sprintf(`rate(collapselab_requests_total{scenario=%q}[1s])`, scenario),
		trigger.Start.Add(-preTriggerWindow),
		rateEnd,
		100*time.Millisecond,
	)
	if err != nil {
		return CollectedTrial{}, fmt.Errorf("query %s request rate: %w", scenario, err)
	}
	inflightRaw, err := l.queryRange(
		ctx,
		fmt.Sprintf(`collapselab_inflight_requests{scenario=%q}`, scenario),
		trigger.Start.Add(-100*time.Millisecond),
		trigger.End.Add(100*time.Millisecond),
		100*time.Millisecond,
	)
	if err != nil {
		return CollectedTrial{}, fmt.Errorf("query %s in-flight: %w", scenario, err)
	}

	summary, err := os.ReadFile(hostSummaryPath)
	if err != nil {
		return CollectedTrial{}, fmt.Errorf("read %s k6 summary: %w", scenario, err)
	}

	return CollectedTrial{
		K6Summary:      summary,
		TriggerState:   triggerRaw,
		RateMatrix:     rateRaw,
		InflightMatrix: inflightRaw,
		Timeline: []TimelineEvent{
			{At: workloadStarted, Event: "workload.started", Scenario: scenario},
			{At: trafficDetectedAt, Event: "workload.traffic_detected", Scenario: scenario},
			{At: scheduledAt, Event: "stall.scheduled", Scenario: scenario, Data: scheduleRaw},
			{At: trigger.Start, Event: "stall.started", Scenario: scenario},
			{At: trigger.End, Event: "stall.ended", Scenario: scenario},
			{At: workloadEnded, Event: "workload.ended", Scenario: scenario},
		},
	}, nil
}

func (l *DockerCF001Lab) repoRoot() (string, error) {
	root := strings.TrimSpace(l.RepoRoot)
	if root == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	return absolute, nil
}

func (l *DockerCF001Lab) client() *http.Client {
	if l.HTTPClient != nil {
		return l.HTTPClient
	}
	return &http.Client{Timeout: 3 * time.Second}
}

func (l *DockerCF001Lab) compose(ctx context.Context, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	_, err := l.commandOutput(ctx, "docker", fullArgs...)
	return err
}

func (l *DockerCF001Lab) commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	root, err := l.repoRoot()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (l *DockerCF001Lab) ensureImage(ctx context.Context, ref string) error {
	if _, err := l.commandOutput(ctx, "docker", "image", "inspect", ref, "--format", "{{.Id}}"); err == nil {
		return nil
	}
	if _, err := l.commandOutput(ctx, "docker", "pull", ref); err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	return nil
}

func (l *DockerCF001Lab) resolvedImageIdentity(ctx context.Context, ref string) (string, error) {
	digest, err := l.commandOutput(ctx, "docker", "image", "inspect", ref, "--format", "{{join .RepoDigests ","}}")
	if err != nil {
		return "", fmt.Errorf("inspect image %s: %w", ref, err)
	}
	if digest != "" {
		return digest, nil
	}
	id, err := l.commandOutput(ctx, "docker", "image", "inspect", ref, "--format", "{{.Id}}")
	if err != nil {
		return "", fmt.Errorf("inspect image id %s: %w", ref, err)
	}
	return ref + "@" + id, nil
}

func (l *DockerCF001Lab) waitForStatus(
	ctx context.Context,
	endpoint string,
	expected int,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("endpoint %s did not return %d: %w", endpoint, expected, lastErr)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		resp, err := l.client().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == expected {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if err := sleepContext(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
}

func (l *DockerCF001Lab) waitForPrometheusTarget(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return errors.New("Prometheus SUT target did not become healthy")
		}
		data, err := l.get(ctx, prometheusBaseURL+"/api/v1/targets")
		if err == nil {
			var payload struct {
				Status string `json:"status"`
				Data   struct {
					ActiveTargets []struct {
						Labels    map[string]string `json:"labels"`
						Health    string            `json:"health"`
						ScrapeURL string            `json:"scrapeUrl"`
					} `json:"activeTargets"`
				} `json:"data"`
			}
			if json.Unmarshal(data, &payload) == nil {
				for _, target := range payload.Data.ActiveTargets {
					if target.Labels["job"] == "collapselab-sut" &&
						target.Health == "up" &&
						target.ScrapeURL == "http://sut-lab:8080/metrics" {
						return nil
					}
				}
			}
		}
		if err := sleepContext(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
}

func (l *DockerCF001Lab) waitForScenarioTraffic(ctx context.Context, scenario string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	query := fmt.Sprintf(`collapselab_requests_total{scenario=%q}`, scenario)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("no completed %s request observed", scenario)
		}
		value, found, err := l.instantScalar(ctx, query)
		if err == nil && found && value > 0 {
			return nil
		}
		if err := sleepContext(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

func (l *DockerCF001Lab) instantScalar(ctx context.Context, query string) (float64, bool, error) {
	values := url.Values{"query": []string{query}}
	data, err := l.get(ctx, prometheusBaseURL+"/api/v1/query?"+values.Encode())
	if err != nil {
		return 0, false, err
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false, err
	}
	if payload.Status != "success" || payload.Data.ResultType != "vector" || len(payload.Data.Result) == 0 {
		return 0, false, nil
	}
	if len(payload.Data.Result[0].Value) != 2 {
		return 0, false, errors.New("unexpected Prometheus vector value")
	}
	var text string
	if err := json.Unmarshal(payload.Data.Result[0].Value[1], &text); err != nil {
		return 0, false, err
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func (l *DockerCF001Lab) scheduleStall(ctx context.Context, after, duration time.Duration) (json.RawMessage, error) {
	values := url.Values{
		"after":    []string{after.String()},
		"duration": []string{duration.String()},
	}
	endpoint := sutControlBaseURL + "/__control/stall?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("schedule stall: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("schedule stall returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.RawMessage(bytes.Clone(data)), nil
}

func (l *DockerCF001Lab) waitForCompletedTrigger(
	ctx context.Context,
	timeout time.Duration,
) ([]byte, TriggerEvidence, error) {
	deadline := time.Now().Add(timeout)
	var last []byte
	for {
		if time.Now().After(deadline) {
			return nil, TriggerEvidence{}, fmt.Errorf("trigger did not complete: %s", strings.TrimSpace(string(last)))
		}
		data, err := l.get(ctx, sutControlBaseURL+"/__control/state")
		if err == nil {
			last = data
			if trigger, parseErr := ParseTriggerEvidence(data); parseErr == nil {
				return data, trigger, nil
			}
		}
		if err := sleepContext(ctx, 100*time.Millisecond); err != nil {
			return nil, TriggerEvidence{}, err
		}
	}
}

func (l *DockerCF001Lab) requireIdleControlState(ctx context.Context) error {
	data, err := l.get(ctx, sutControlBaseURL+"/__control/state")
	if err != nil {
		return fmt.Errorf("read control state: %w", err)
	}
	var state struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode control state: %w", err)
	}
	if state.Active {
		return errors.New("SUT stall gate is already active")
	}
	return nil
}

func (l *DockerCF001Lab) queryRange(
	ctx context.Context,
	query string,
	start, end time.Time,
	step time.Duration,
) ([]byte, error) {
	values := url.Values{
		"query": []string{query},
		"start": []string{formatUnixSeconds(start)},
		"end":   []string{formatUnixSeconds(end)},
		"step":  []string{strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	data, err := l.get(ctx, prometheusBaseURL+"/api/v1/query_range?"+values.Encode())
	if err != nil {
		return nil, err
	}
	if _, err := ParsePrometheusMatrix(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (l *DockerCF001Lab) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (l *DockerCF001Lab) summaryPaths(bundleDir, scenario string) (string, string, error) {
	root, err := l.repoRoot()
	if err != nil {
		return "", "", err
	}
	runsRoot := filepath.Join(root, "runs")
	absoluteBundle, err := filepath.Abs(bundleDir)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(runsRoot, absoluteBundle)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("bundle %s is outside repository runs directory", absoluteBundle)
	}
	hostPath := filepath.Join(absoluteBundle, scenario, "k6-summary.json")
	containerPath := "/runs/" + filepath.ToSlash(filepath.Join(rel, scenario, "k6-summary.json"))
	return containerPath, hostPath, nil
}

func (l *DockerCF001Lab) hostUIDGID(ctx context.Context) (string, string, error) {
	uid, err := l.commandOutput(ctx, "id", "-u")
	if err != nil {
		return "", "", fmt.Errorf("resolve host uid: %w", err)
	}
	gid, err := l.commandOutput(ctx, "id", "-g")
	if err != nil {
		return "", "", fmt.Errorf("resolve host gid: %w", err)
	}
	return uid, gid, nil
}

func formatUnixSeconds(at time.Time) string {
	return strconv.FormatFloat(
		float64(at.UnixNano())/float64(time.Second),
		'f',
		-1,
		64,
	)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
