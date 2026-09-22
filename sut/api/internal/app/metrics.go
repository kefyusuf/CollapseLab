package app

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"collapselab/sut/api/internal/stallgate"
)

type durationObserver interface {
	Observe(float64)
}

func observeElapsed(started time.Time, observer durationObserver) {
	observer.Observe(time.Since(started).Seconds())
}

type Metrics struct {
	Requests        *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	Inflight        *prometheus.GaugeVec
	Stalls          prometheus.Counter
	gatherer        prometheus.Gatherer
}

func NewMetrics(reg prometheus.Registerer, gate *stallgate.Gate) *Metrics {
	metrics := &Metrics{
		Requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "collapselab_requests_total",
				Help: "Completed CollapseLab SUT work requests.",
			},
			[]string{"scenario"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "collapselab_request_duration_seconds",
				Help:    "End-to-end SUT work request duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"scenario"},
		),
		Inflight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "collapselab_inflight_requests",
				Help: "Current CollapseLab SUT work requests admitted to the handler.",
			},
			[]string{"scenario"},
		),
		Stalls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "collapselab_stalls_total",
			Help: "Accepted deterministic stall schedules.",
		}),
	}

	stallActive := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "collapselab_stall_active",
			Help: "Whether the deterministic stall gate is currently active.",
		},
		func() float64 {
			if gate.Active() {
				return 1
			}
			return 0
		},
	)

	for _, scenario := range []string{"closed", "open", "unknown"} {
		metrics.Requests.WithLabelValues(scenario)
		metrics.RequestDuration.WithLabelValues(scenario)
		metrics.Inflight.WithLabelValues(scenario)
	}

	reg.MustRegister(metrics.Requests, metrics.RequestDuration, metrics.Inflight, metrics.Stalls, stallActive)
	if gatherer, ok := reg.(prometheus.Gatherer); ok {
		metrics.gatherer = gatherer
	}
	return metrics
}
