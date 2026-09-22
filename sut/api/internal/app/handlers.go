package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"collapselab/sut/api/internal/stallgate"
)

const (
	maxControlAfter    = 25 * time.Second
	maxControlDuration = 5 * time.Second
)

type scheduleResponse struct {
	Generation  uint64    `json:"generation"`
	ScheduledAt time.Time `json:"scheduled_at"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

func NewWorkHandler(baseServiceTime time.Duration, gate *stallgate.Gate, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		scenario := boundedScenario(r.Header.Get("X-CollapseLab-Scenario"))
		started := time.Now()
		inflight := metrics.Inflight.WithLabelValues(scenario)
		durationObserver := metrics.RequestDuration.WithLabelValues(scenario)
		inflight.Inc()
		defer inflight.Dec()
		defer func() { observeElapsed(started, durationObserver) }()

		if err := gate.Wait(r.Context()); err != nil {
			return
		}

		time.Sleep(baseServiceTime)
		metrics.Requests.WithLabelValues(scenario).Inc()
		w.WriteHeader(http.StatusNoContent)
	})
}

func NewControlHandler(gate *stallgate.Gate, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__control/stall":
			handleSchedule(w, r, gate, metrics)
		case "/__control/state":
			handleState(w, r, gate)
		default:
			http.NotFound(w, r)
		}
	})
}

func handleSchedule(w http.ResponseWriter, r *http.Request, gate *stallgate.Gate, metrics *Metrics) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	after, err := parseDurationParam(r, "after")
	if err != nil || after < 0 || after > maxControlAfter {
		http.Error(w, "invalid after duration", http.StatusBadRequest)
		return
	}
	duration, err := parseDurationParam(r, "duration")
	if err != nil || duration <= 0 || duration > maxControlDuration {
		http.Error(w, "invalid stall duration", http.StatusBadRequest)
		return
	}

	scheduledAt, startsAt, endsAt, err := gate.Schedule(after, duration)
	if errors.Is(err, stallgate.ErrAlreadyStalled) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	metrics.Stalls.Inc()
	state := gate.State()
	writeJSON(w, http.StatusAccepted, scheduleResponse{
		Generation:  state.Generation,
		ScheduledAt: scheduledAt,
		StartsAt:    startsAt,
		EndsAt:      endsAt,
	})
}

func handleState(w http.ResponseWriter, r *http.Request, gate *stallgate.Gate) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, gate.State())
}

func parseDurationParam(r *http.Request, name string) (time.Duration, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, errors.New("missing duration")
	}
	return time.ParseDuration(raw)
}

func boundedScenario(raw string) string {
	switch raw {
	case "closed", "open":
		return raw
	default:
		return "unknown"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
