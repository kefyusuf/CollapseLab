package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"collapselab/sut/api/internal/stallgate"
)

func TestNewServersUseSeparateFixedListeners(t *testing.T) {
	gate := stallgate.New()
	metrics := testMetrics(gate)
	public, control := NewServers(5*time.Millisecond, gate, metrics)

	if public.Addr != ":8080" {
		t.Fatalf("public addr = %q", public.Addr)
	}
	if control.Addr != ":9091" {
		t.Fatalf("control addr = %q", control.Addr)
	}

	controlReq := httptest.NewRequest(http.MethodGet, "/__control/state", nil)
	publicRec := httptest.NewRecorder()
	public.Handler.ServeHTTP(publicRec, controlReq)
	if publicRec.Code != http.StatusNotFound {
		t.Fatalf("control route leaked onto public listener: %d", publicRec.Code)
	}

	workReq := httptest.NewRequest(http.MethodGet, "/work", nil)
	controlRec := httptest.NewRecorder()
	control.Handler.ServeHTTP(controlRec, workReq)
	if controlRec.Code != http.StatusNotFound {
		t.Fatalf("work route leaked onto control listener: %d", controlRec.Code)
	}
}

func TestServeUntilCancelledReturnsAfterBoundedShutdown(t *testing.T) {
	public := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	control := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)

	started := time.Now()
	if err := ServeUntilCancelled(ctx, public, control); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("shutdown took too long: %s", elapsed)
	}
}
