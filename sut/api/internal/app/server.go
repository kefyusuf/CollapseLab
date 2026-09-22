package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"collapselab/sut/api/internal/stallgate"
)

func NewPublicHandler(baseServiceTime time.Duration, gate *stallgate.Gate, metrics *Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/work", NewWorkHandler(baseServiceTime, gate, metrics))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if metrics.gatherer != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(metrics.gatherer, promhttp.HandlerOpts{}))
	}
	return mux
}

func NewControlPlaneHandler(gate *stallgate.Gate, metrics *Metrics) http.Handler {
	return NewControlHandler(gate, metrics)
}

func NewServers(baseServiceTime time.Duration, gate *stallgate.Gate, metrics *Metrics) (public, control *http.Server) {
	public = &http.Server{
		Addr:              ":8080",
		Handler:           NewPublicHandler(baseServiceTime, gate, metrics),
		ReadHeaderTimeout: 5 * time.Second,
	}
	control = &http.Server{
		Addr:              ":9091",
		Handler:           NewControlPlaneHandler(gate, metrics),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return public, control
}

const shutdownTimeout = 5 * time.Second

func ServeUntilCancelled(ctx context.Context, public, control *http.Server) error {
	results := make(chan error, 2)
	serve := func(server *http.Server) {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		results <- err
	}

	go serve(public)
	go serve(control)

	remaining := 2
	var firstErr error
	select {
	case <-ctx.Done():
	case firstErr = <-results:
		remaining = 1
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := errors.Join(public.Shutdown(shutdownCtx), control.Shutdown(shutdownCtx))

	var serveErr error
	for i := 0; i < remaining; i++ {
		serveErr = errors.Join(serveErr, <-results)
	}

	return errors.Join(firstErr, shutdownErr, serveErr)
}
