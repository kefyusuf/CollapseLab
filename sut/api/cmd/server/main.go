package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"collapselab/sut/api/internal/app"
	"collapselab/sut/api/internal/stallgate"
)

func main() {
	baseServiceTime, err := baseServiceTimeFromEnv(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	gate := stallgate.New()
	registry := prometheus.NewRegistry()
	metrics := app.NewMetrics(registry, gate)
	public, control := app.NewServers(baseServiceTime, gate, metrics)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.ServeUntilCancelled(ctx, public, control); err != nil {
		log.Fatal(err)
	}
}

func baseServiceTimeFromEnv(getenv func(string) string) (time.Duration, error) {
	raw := getenv("BASE_SERVICE_TIME")
	if raw == "" {
		return 0, errors.New("BASE_SERVICE_TIME is required")
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, errors.New("BASE_SERVICE_TIME must be positive")
	}
	return duration, nil
}
