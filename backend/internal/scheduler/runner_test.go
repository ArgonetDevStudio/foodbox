package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mutex  sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func (clock *fakeClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *fakeClock) NewTimer(delay time.Duration) Timer {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	timer := &fakeTimer{channel: make(chan time.Time, 1)}
	clock.timers = append(clock.timers, timer)
	return timer
}

func (clock *fakeClock) fire(at time.Time) {
	clock.mutex.Lock()
	clock.now = at
	timer := clock.timers[len(clock.timers)-1]
	clock.mutex.Unlock()
	timer.channel <- at
}

func (clock *fakeClock) timerCount() int {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return len(clock.timers)
}

type fakeTimer struct {
	channel chan time.Time
}

func (timer *fakeTimer) C() <-chan time.Time { return timer.channel }
func (timer *fakeTimer) Stop() bool          { return true }

func TestRunnerWaitsForReadinessBeforeStartup(t *testing.T) {
	location := SeoulLocation()
	clock := &fakeClock{now: time.Date(2026, time.July, 25, 8, 0, 0, 0, location)}
	started := make(chan struct{})
	runner, err := NewRunner(Options{
		Clock:   clock,
		Startup: func(context.Context) error { close(started); return nil },
		Daily:   func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, ready) }()

	select {
	case <-started:
		t.Fatal("startup ran before server readiness")
	case <-time.After(20 * time.Millisecond):
	}

	close(ready)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("startup did not run after server readiness")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerPreventsStartupAndDailyOverlap(t *testing.T) {
	location := SeoulLocation()
	initial := time.Date(2026, time.July, 25, 8, 0, 0, 0, location)
	clock := &fakeClock{now: initial}
	startupStarted := make(chan struct{})
	releaseStartup := make(chan struct{})
	dailyCalled := make(chan struct{}, 1)
	runner, err := NewRunner(Options{
		Clock: clock,
		Startup: func(context.Context) error {
			close(startupStarted)
			<-releaseStartup
			return nil
		},
		Daily: func(context.Context) error {
			dailyCalled <- struct{}{}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan struct{})
	close(ready)
	go func() { done <- runner.Run(ctx, ready) }()

	select {
	case <-startupStarted:
	case <-time.After(time.Second):
		t.Fatal("startup did not start")
	}
	waitForTimers(t, clock, 1)
	clock.fire(time.Date(2026, time.July, 25, 9, 0, 0, 0, location))

	select {
	case <-dailyCalled:
		t.Fatal("daily job overlapped startup job")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseStartup)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerCancelsAndWaitsForActiveJob(t *testing.T) {
	location := SeoulLocation()
	clock := &fakeClock{now: time.Date(2026, time.July, 25, 8, 0, 0, 0, location)}
	jobStopped := make(chan struct{})
	runner, err := NewRunner(Options{
		Clock: clock,
		Startup: func(ctx context.Context) error {
			<-ctx.Done()
			close(jobStopped)
			return ctx.Err()
		},
		Daily: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	close(ready)
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, ready) }()
	waitForTimers(t, clock, 1)
	cancel()

	select {
	case <-jobStopped:
	case <-time.After(time.Second):
		t.Fatal("active job was not cancelled")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not wait for active job shutdown")
	}
}

func waitForTimers(t *testing.T, clock *fakeClock, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if clock.timerCount() >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timer count = %d, want at least %d", clock.timerCount(), count)
}
