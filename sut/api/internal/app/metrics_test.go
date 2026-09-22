package app

import (
	"testing"
	"time"
)

type recordingObserver struct {
	value float64
}

func (o *recordingObserver) Observe(value float64) {
	o.value = value
}

func TestObserveElapsedMeasuresAtCompletionTime(t *testing.T) {
	observer := &recordingObserver{}
	started := time.Now()
	time.Sleep(15 * time.Millisecond)

	observeElapsed(started, observer)

	if observer.value < 0.010 {
		t.Fatalf("observed duration = %f, want at least 0.010s", observer.value)
	}
}
