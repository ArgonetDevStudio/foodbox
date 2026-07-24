package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Job is a startup or scheduled unit of work. Implementations should return
// promptly when ctx is cancelled so Runner can shut down cleanly.
type Job func(ctx context.Context) error

// Options configures a daily runner. NewRunner uses the Foodbox default of
// 09:00; use NewRunnerAt to schedule a different time.
type Options struct {
	Clock    Clock
	Location *time.Location
	Startup  Job
	Daily    Job
	OnError  func(error)
}

// Runner starts work only after the HTTP server signals readiness. Startup and
// Daily jobs share a single-flight guard, preventing overlapping crawl/notify
// operations.
type Runner struct {
	clock     Clock
	location  *time.Location
	hour      int
	minute    int
	startup   Job
	daily     Job
	onError   func(error)
	stateMu   sync.Mutex
	running   bool
	pending   bool
	waitGroup sync.WaitGroup
}

// NewRunner creates the standard Foodbox runner scheduled for 09:00 in Seoul.
func NewRunner(options Options) (*Runner, error) {
	return NewRunnerAt(defaultHour, 0, options)
}

// NewRunnerAt is primarily useful for tests and explicitly different schedules.
func NewRunnerAt(hour, minute int, options Options) (*Runner, error) {
	if hour < 0 || hour > 23 {
		return nil, fmt.Errorf("scheduler hour must be between 0 and 23: %d", hour)
	}
	if minute < 0 || minute > 59 {
		return nil, fmt.Errorf("scheduler minute must be between 0 and 59: %d", minute)
	}
	if options.Daily == nil {
		return nil, errors.New("scheduler daily job is required")
	}
	if options.Clock == nil {
		options.Clock = systemClock{}
	}
	if options.Location == nil {
		options.Location = SeoulLocation()
	}
	if options.OnError == nil {
		options.OnError = func(error) {}
	}

	return &Runner{
		clock:    options.Clock,
		location: options.Location,
		hour:     hour,
		minute:   minute,
		startup:  options.Startup,
		daily:    options.Daily,
		onError:  options.OnError,
	}, nil
}

// Run waits for ready, invokes Startup once in the background, and then runs
// Daily at each scheduled time. Closing or cancelling ctx stops the timer,
// cancels active work, and waits for that work to return.
func (runner *Runner) Run(ctx context.Context, ready <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return nil
	case <-ready:
	}

	workContext, cancelWork := context.WithCancel(ctx)
	defer func() {
		cancelWork()
		runner.waitGroup.Wait()
	}()

	if runner.startup != nil {
		runner.start(workContext, runner.startup)
	}

	for {
		now := runner.clock.Now()
		next := NextDailyRun(now, runner.location, runner.hour, runner.minute)
		timer := runner.clock.NewTimer(next.Sub(now))

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C():
			runner.scheduleDaily(workContext)
		}
	}
}

func (runner *Runner) start(ctx context.Context, job Job) bool {
	runner.stateMu.Lock()
	defer runner.stateMu.Unlock()
	if runner.running || ctx.Err() != nil {
		return false
	}
	runner.running = true

	runner.waitGroup.Add(1)
	go runner.run(ctx, job)
	return true
}

func (runner *Runner) scheduleDaily(ctx context.Context) {
	runner.stateMu.Lock()
	if ctx.Err() != nil {
		runner.stateMu.Unlock()
		return
	}
	if runner.running {
		runner.pending = true
		runner.stateMu.Unlock()
		return
	}
	runner.running = true
	runner.waitGroup.Add(1)
	runner.stateMu.Unlock()

	go runner.run(ctx, runner.daily)
}

func (runner *Runner) run(ctx context.Context, job Job) {
	defer runner.waitGroup.Done()

	for {
		if err := job(ctx); err != nil && !errors.Is(err, context.Canceled) {
			runner.onError(err)
		}

		runner.stateMu.Lock()
		if runner.pending && ctx.Err() == nil {
			runner.pending = false
			runner.stateMu.Unlock()
			job = runner.daily
			continue
		}
		runner.pending = false
		runner.running = false
		runner.stateMu.Unlock()
		return
	}
}
