package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"collapselab/sut/api/internal/stallgate"
)

func TestWorkHandlerSleepsBaseServiceTime(t *testing.T) {
	h := NewWorkHandler(20*time.Millisecond, stallgate.New(), testMetrics())
	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-CollapseLab-Scenario", "closed")
	rec := httptest.NewRecorder()

	started := time.Now()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("request completed too quickly: %s", elapsed)
	}
}

func TestControlHandlerRejectsExcessiveStall(t *testing.T) {
	h := NewControlHandler(stallgate.New(), testMetrics())
	req := httptest.NewRequest(http.MethodPost, "/__control/stall?after=0s&duration=6s", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestControlHandlerRejectsNegativeAfter(t *testing.T) {
	h := NewControlHandler(stallgate.New(), testMetrics())
	req := httptest.NewRequest(http.MethodPost, "/__control/stall?after=-1ms&duration=100ms", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestControlHandlerSchedulesFutureStall(t *testing.T) {
	h := NewControlHandler(stallgate.New(), testMetrics())
	req := httptest.NewRequest(http.MethodPost, "/__control/stall?after=50ms&duration=80ms", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Generation  uint64    `json:"generation"`
		ScheduledAt time.Time `json:"scheduled_at"`
		StartsAt    time.Time `json:"starts_at"`
		EndsAt      time.Time `json:"ends_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Generation != 1 {
		t.Fatalf("generation = %d, want 1", response.Generation)
	}
	if !response.EndsAt.After(response.StartsAt) {
		t.Fatalf("invalid planned window: %s -> %s", response.StartsAt, response.EndsAt)
	}
	if response.ScheduledAt.IsZero() {
		t.Fatal("missing scheduled_at")
	}
}

func TestControlHandlerRejectsOverlappingSchedule(t *testing.T) {
	gate := stallgate.New()
	h := NewControlHandler(gate, testMetrics())
	first := httptest.NewRequest(http.MethodPost, "/__control/stall?after=100ms&duration=100ms", nil)
	firstRec := httptest.NewRecorder()
	h.ServeHTTP(firstRec, first)

	second := httptest.NewRequest(http.MethodPost, "/__control/stall?after=100ms&duration=100ms", nil)
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("status = %d", secondRec.Code)
	}
}

func TestControlStateReportsGateState(t *testing.T) {
	gate := stallgate.New()
	_, _, _, err := gate.Schedule(0, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	h := NewControlHandler(gate, testMetrics())
	req := httptest.NewRequest(http.MethodGet, "/__control/state", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var state stallgate.State
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.ActualStartedAt == nil {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestPublicAndControlRoutesAreSeparated(t *testing.T) {
	gate := stallgate.New()
	metrics := testMetrics()
	public := NewPublicHandler(5*time.Millisecond, gate, metrics)
	control := NewControlPlaneHandler(gate, metrics)

	publicControlReq := httptest.NewRequest(http.MethodGet, "/__control/state", nil)
	publicControlRec := httptest.NewRecorder()
	public.ServeHTTP(publicControlRec, publicControlReq)
	if publicControlRec.Code != http.StatusNotFound {
		t.Fatalf("public control status = %d", publicControlRec.Code)
	}

	controlWorkReq := httptest.NewRequest(http.MethodGet, "/work", nil)
	controlWorkRec := httptest.NewRecorder()
	control.ServeHTTP(controlWorkRec, controlWorkReq)
	if controlWorkRec.Code != http.StatusNotFound {
		t.Fatalf("control work status = %d", controlWorkRec.Code)
	}
}

func testMetrics(gates ...*stallgate.Gate) *Metrics {
	gate := stallgate.New()
	if len(gates) > 0 && gates[0] != nil {
		gate = gates[0]
	}
	return NewMetrics(prometheus.NewRegistry(), gate)
}

func TestBoundedScenarioRejectsUnboundedLabels(t *testing.T) {
	cases := map[string]string{
		"closed":       "closed",
		"open":         "open",
		"":             "unknown",
		"customer-123": "unknown",
	}
	for input, want := range cases {
		if got := boundedScenario(input); got != want {
			t.Fatalf("boundedScenario(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPublicHealthAndMetricsEndpoints(t *testing.T) {
	gate := stallgate.New()
	metrics := testMetrics(gate)
	public := NewPublicHandler(5*time.Millisecond, gate, metrics)

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	public.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusNoContent {
		t.Fatalf("health status = %d", healthRec.Code)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	public.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", metricsRec.Code)
	}
}
