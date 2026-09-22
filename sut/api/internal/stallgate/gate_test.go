package stallgate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGateBlocksUntilStallEnds(t *testing.T) {
	gate := New()
	_, start, end, err := gate.Schedule(0, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	began := time.Now()
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	if waited := time.Since(began); waited < 60*time.Millisecond {
		t.Fatalf("waited only %s; stall was not enforced", waited)
	}
	if !end.After(start) {
		t.Fatalf("invalid stall window: %s -> %s", start, end)
	}
}

func TestGateWaitHonorsContextCancellation(t *testing.T) {
	gate := New()
	_, _, _, err := gate.Schedule(0, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := gate.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestScheduleRejectsOverlapAndIncrementsGeneration(t *testing.T) {
	gate := New()
	_, _, _, err := gate.Schedule(50*time.Millisecond, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	first := gate.State()
	if first.Generation != 1 {
		t.Fatalf("generation = %d, want 1", first.Generation)
	}

	if _, _, _, err := gate.Schedule(0, 50*time.Millisecond); !errors.Is(err, ErrAlreadyStalled) {
		t.Fatalf("expected ErrAlreadyStalled, got %v", err)
	}
}

func TestZeroDelayScheduleIsActiveBeforeReturn(t *testing.T) {
	gate := New()
	_, _, _, err := gate.Schedule(0, 60*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	state := gate.State()
	if !state.Active {
		t.Fatal("expected active stall")
	}
	if state.ActualStartedAt == nil {
		t.Fatal("expected actual start timestamp")
	}
}

func TestFutureScheduleRecordsActualStartAndEnd(t *testing.T) {
	gate := New()
	_, plannedStart, _, err := gate.Schedule(20*time.Millisecond, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		state := gate.State()
		if state.ActualEndedAt != nil {
			if state.ActualStartedAt == nil {
				t.Fatal("actual end recorded without actual start")
			}
			if state.ActualStartedAt.Before(plannedStart.Add(-20 * time.Millisecond)) {
				t.Fatalf("actual start %s is implausibly before planned start %s", state.ActualStartedAt, plannedStart)
			}
			if state.Active {
				t.Fatal("stall still active after actual end")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stall did not complete; state=%+v", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
