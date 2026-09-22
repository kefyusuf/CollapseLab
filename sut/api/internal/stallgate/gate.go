package stallgate

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrAlreadyStalled = errors.New("stall already active")
var ErrInvalidDuration = errors.New("stall duration must be positive")

type State struct {
	Generation      uint64     `json:"generation"`
	ScheduledAt     time.Time  `json:"scheduled_at"`
	PlannedStartsAt time.Time  `json:"planned_starts_at"`
	PlannedEndsAt   time.Time  `json:"planned_ends_at"`
	ActualStartedAt *time.Time `json:"actual_started_at"`
	ActualEndedAt   *time.Time `json:"actual_ended_at"`
	Active          bool       `json:"active"`
}

type Gate struct {
	mu           sync.Mutex
	blockedUntil time.Time
	wake         chan struct{}
	scheduled    bool
	state        State
}

func New() *Gate {
	return &Gate{}
}

func (g *Gate) Schedule(after, duration time.Duration) (scheduledAt, startsAt, endsAt time.Time, err error) {
	if duration <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, ErrInvalidDuration
	}

	scheduledAt = time.Now().UTC()
	startsAt = scheduledAt.Add(after)
	endsAt = startsAt.Add(duration)

	g.mu.Lock()
	if g.scheduled || g.state.Active {
		g.mu.Unlock()
		return time.Time{}, time.Time{}, time.Time{}, ErrAlreadyStalled
	}

	g.scheduled = true
	g.wake = make(chan struct{})
	g.state.Generation++
	g.state.ScheduledAt = scheduledAt
	g.state.PlannedStartsAt = startsAt
	g.state.PlannedEndsAt = endsAt
	g.state.ActualStartedAt = nil
	g.state.ActualEndedAt = nil
	g.state.Active = false
	generation := g.state.Generation

	if after <= 0 {
		now := time.Now().UTC()
		g.state.Active = true
		g.state.ActualStartedAt = timePtr(now)
		g.blockedUntil = now.Add(duration)
		g.mu.Unlock()
		go g.finishAfter(generation, duration)
		return scheduledAt, startsAt, endsAt, nil
	}
	g.mu.Unlock()

	go g.startAfter(generation, after, duration)
	return scheduledAt, startsAt, endsAt, nil
}

func (g *Gate) startAfter(generation uint64, after, duration time.Duration) {
	timer := time.NewTimer(after)
	defer timer.Stop()
	<-timer.C

	g.mu.Lock()
	if !g.scheduled || g.state.Generation != generation || g.state.ActualEndedAt != nil {
		g.mu.Unlock()
		return
	}

	now := time.Now().UTC()
	g.state.Active = true
	g.state.ActualStartedAt = timePtr(now)
	g.blockedUntil = now.Add(duration)
	g.mu.Unlock()

	g.finishAfter(generation, duration)
}

func (g *Gate) finishAfter(generation uint64, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	<-timer.C

	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.scheduled || g.state.Generation != generation {
		return
	}

	now := time.Now().UTC()
	g.state.Active = false
	g.state.ActualEndedAt = timePtr(now)
	g.blockedUntil = time.Time{}
	g.scheduled = false
	if g.wake != nil {
		close(g.wake)
		g.wake = nil
	}
}

func (g *Gate) Wait(ctx context.Context) error {
	g.mu.Lock()
	if !g.state.Active {
		g.mu.Unlock()
		return nil
	}
	wake := g.wake
	g.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	}
}

func (g *Gate) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state.Active
}

func (g *Gate) State() State {
	g.mu.Lock()
	defer g.mu.Unlock()
	return cloneState(g.state)
}

func cloneState(state State) State {
	if state.ActualStartedAt != nil {
		started := *state.ActualStartedAt
		state.ActualStartedAt = &started
	}
	if state.ActualEndedAt != nil {
		ended := *state.ActualEndedAt
		state.ActualEndedAt = &ended
	}
	return state
}

func timePtr(value time.Time) *time.Time {
	return &value
}
