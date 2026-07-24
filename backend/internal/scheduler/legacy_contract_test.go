package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
	"github.com/LooLookProject/foodbox/backend/internal/service"
)

func TestLegacyContractNextRunIsExactlyNineAMInSeoul(t *testing.T) {
	seoul := SeoulLocation()
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "one nanosecond before nine runs today",
			now:  time.Date(2026, time.July, 25, 8, 59, 59, 999999999, seoul),
			want: time.Date(2026, time.July, 25, 9, 0, 0, 0, seoul),
		},
		{
			name: "exactly nine runs on the next calendar day",
			now:  time.Date(2026, time.July, 25, 9, 0, 0, 0, seoul),
			want: time.Date(2026, time.July, 26, 9, 0, 0, 0, seoul),
		},
		{
			name: "midnight still runs that morning",
			now:  time.Date(2026, time.July, 31, 0, 0, 0, 0, seoul),
			want: time.Date(2026, time.July, 31, 9, 0, 0, 0, seoul),
		},
		{
			name: "month boundary",
			now:  time.Date(2026, time.July, 31, 23, 59, 59, 0, seoul),
			want: time.Date(2026, time.August, 1, 9, 0, 0, 0, seoul),
		},
		{
			name: "leap day boundary",
			now:  time.Date(2024, time.February, 28, 9, 0, 0, 0, seoul),
			want: time.Date(2024, time.February, 29, 9, 0, 0, 0, seoul),
		},
		{
			name: "year boundary",
			now:  time.Date(2026, time.December, 31, 23, 59, 59, 0, seoul),
			want: time.Date(2027, time.January, 1, 9, 0, 0, 0, seoul),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NextDailyRun(test.now, seoul, 9, 0)
			if !got.Equal(test.want) {
				t.Fatalf("NextDailyRun(%s) = %s, want %s", test.now, got, test.want)
			}
			if got.Location().String() != "Asia/Seoul" || got.Hour() != 9 || got.Minute() != 0 || got.Second() != 0 {
				t.Fatalf("next run is not exactly 09:00 Asia/Seoul: %s", got)
			}
		})
	}
}

func TestLegacyContractSeoulScheduleIgnoresHostTimezoneAndDST(t *testing.T) {
	seoul := SeoulLocation()
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "US spring DST transition",
			now:  time.Date(2026, time.March, 8, 1, 59, 59, 0, time.FixedZone("host-before-DST", -5*60*60)),
			want: time.Date(2026, time.March, 9, 9, 0, 0, 0, seoul),
		},
		{
			name: "US autumn DST transition",
			now:  time.Date(2026, time.November, 1, 1, 0, 0, 0, time.FixedZone("host-after-DST", -5*60*60)),
			want: time.Date(2026, time.November, 2, 9, 0, 0, 0, seoul),
		},
		{
			name: "host is a day behind Seoul",
			now:  time.Date(2026, time.July, 24, 13, 59, 59, 0, time.FixedZone("host-Honolulu", -10*60*60)),
			want: time.Date(2026, time.July, 25, 9, 0, 0, 0, seoul),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NextDailyRun(test.now, seoul, 9, 0)
			if !got.Equal(test.want) {
				t.Fatalf("NextDailyRun(%s) = %s, want %s", test.now, got, test.want)
			}
		})
	}
}

func TestLegacyContractRestartImmediatelyBeforeNineQueuesOneDailyRun(t *testing.T) {
	seoul := SeoulLocation()
	clock := newContractClock(time.Date(2026, time.July, 27, 8, 59, 59, 0, seoul))
	startupStarted := make(chan struct{})
	releaseStartup := make(chan struct{})
	dailyStarted := make(chan struct{}, 2)
	releaseDaily := make(chan struct{})

	runner, err := NewRunner(Options{
		Clock: clock,
		Startup: func(ctx context.Context) error {
			close(startupStarted)
			select {
			case <-releaseStartup:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		Daily: func(ctx context.Context) error {
			dailyStarted <- struct{}{}
			select {
			case <-releaseDaily:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cancel, done := runContractRunner(t, runner)
	defer stopContractRunner(t, cancel, done)
	waitContractSignal(t, startupStarted, "startup did not start")
	waitContractTimerCount(t, clock, 1)
	if got := clock.delay(0); got != time.Second {
		t.Fatalf("first timer delay = %s, want 1s", got)
	}

	clock.fireLatest(time.Date(2026, time.July, 27, 9, 0, 0, 0, seoul))
	waitContractTimerCount(t, clock, 2)
	clock.fireLatest(time.Date(2026, time.July, 28, 9, 0, 0, 0, seoul))
	waitContractTimerCount(t, clock, 3)

	select {
	case <-dailyStarted:
		t.Fatal("daily notification overlapped startup refresh")
	default:
	}
	close(releaseStartup)
	waitContractSignal(t, dailyStarted, "pending 09:00 notification did not run after startup")
	close(releaseDaily)

	select {
	case <-dailyStarted:
		t.Fatal("overlapping timer ticks were not coalesced to one notification")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestLegacyContractRestartExactlyAtNineDoesNotBackfill(t *testing.T) {
	seoul := SeoulLocation()
	clock := newContractClock(time.Date(2026, time.July, 27, 9, 0, 0, 0, seoul))
	dailyCalls := make(chan struct{}, 1)
	runner, err := NewRunner(Options{
		Clock: clock,
		Daily: func(context.Context) error {
			dailyCalls <- struct{}{}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cancel, done := runContractRunner(t, runner)
	defer stopContractRunner(t, cancel, done)
	waitContractTimerCount(t, clock, 1)
	if got := clock.delay(0); got != 24*time.Hour {
		t.Fatalf("timer delay at exact 09:00 = %s, want 24h", got)
	}
	select {
	case <-dailyCalls:
		t.Fatal("restart at exact 09:00 unexpectedly backfilled a notification")
	default:
	}
}

func TestLegacyContractWeekendTicksCallNeitherMenuNorSlack(t *testing.T) {
	seoul := SeoulLocation()
	for _, date := range []time.Time{
		time.Date(2026, time.July, 25, 8, 0, 0, 0, seoul),
		time.Date(2026, time.July, 26, 8, 0, 0, 0, seoul),
	} {
		date := date
		t.Run(date.Weekday().String(), func(t *testing.T) {
			clock := newContractClock(date)
			provider := &contractMenuProvider{}
			sender := &contractSlackSender{}
			notifications := service.NewNotificationService(
				provider,
				sender,
				service.NotificationConfig{},
				service.WithNotificationDateClock(func() domain.LocalDate { return contractLocalDate(clock.Now(), seoul) }),
			)
			completed := make(chan error, 1)
			runner, err := NewRunner(Options{
				Clock: clock,
				Daily: func(ctx context.Context) error {
					err := notifications.NotifyToday(ctx)
					completed <- err
					return err
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			cancel, done := runContractRunner(t, runner)
			defer stopContractRunner(t, cancel, done)
			waitContractTimerCount(t, clock, 1)
			clock.fireLatest(time.Date(date.Year(), date.Month(), date.Day(), 9, 0, 0, 0, seoul))
			select {
			case err := <-completed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("weekend scheduled job did not complete")
			}
			if provider.callCount() != 0 || sender.callCount() != 0 {
				t.Fatalf("weekend calls: menu=%d slack=%d, want both zero", provider.callCount(), sender.callCount())
			}
		})
	}
}

func TestLegacyContractWeekdayTickSendsExactlyOnce(t *testing.T) {
	seoul := SeoulLocation()
	date := time.Date(2026, time.July, 27, 8, 0, 0, 0, seoul)
	clock := newContractClock(date)
	provider := &contractMenuProvider{menu: domain.Menu{
		Date:  domain.LocalDate{Year: 2026, Month: 7, Day: 27},
		Menus: []string{"menu one", "menu two", "menu three"},
		Valid: true,
	}}
	sender := &contractSlackSender{}
	notifications := service.NewNotificationService(
		provider,
		sender,
		service.NotificationConfig{},
		service.WithNotificationDateClock(func() domain.LocalDate { return contractLocalDate(clock.Now(), seoul) }),
	)
	completed := make(chan error, 1)
	runner, err := NewRunner(Options{
		Clock: clock,
		Daily: func(ctx context.Context) error {
			err := notifications.NotifyToday(ctx)
			completed <- err
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cancel, done := runContractRunner(t, runner)
	defer stopContractRunner(t, cancel, done)
	waitContractTimerCount(t, clock, 1)
	clock.fireLatest(time.Date(2026, time.July, 27, 9, 0, 0, 0, seoul))
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("weekday scheduled notification did not complete")
	}
	if provider.callCount() != 1 || sender.callCount() != 1 {
		t.Fatalf("weekday calls: menu=%d slack=%d, want exactly one each", provider.callCount(), sender.callCount())
	}
}

type contractClock struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
	timers []*contractTimer
}

func newContractClock(now time.Time) *contractClock {
	return &contractClock{now: now}
}

func (clock *contractClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *contractClock) NewTimer(delay time.Duration) Timer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := &contractTimer{channel: make(chan time.Time, 1)}
	clock.delays = append(clock.delays, delay)
	clock.timers = append(clock.timers, timer)
	return timer
}

func (clock *contractClock) fireLatest(at time.Time) {
	clock.mu.Lock()
	clock.now = at
	timer := clock.timers[len(clock.timers)-1]
	clock.mu.Unlock()
	timer.channel <- at
}

func (clock *contractClock) timerCount() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return len(clock.timers)
}

func (clock *contractClock) delay(index int) time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.delays[index]
}

type contractTimer struct {
	channel chan time.Time
}

func (timer *contractTimer) C() <-chan time.Time { return timer.channel }
func (timer *contractTimer) Stop() bool          { return true }

type contractMenuProvider struct {
	mu    sync.Mutex
	calls int
	menu  domain.Menu
}

func (provider *contractMenuProvider) Today(context.Context, domain.LocalDate) (domain.Menu, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	return provider.menu, nil
}

func (provider *contractMenuProvider) callCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

type contractSlackSender struct {
	mu    sync.Mutex
	calls int
}

func (sender *contractSlackSender) Send(context.Context, service.SlackMessage) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	sender.calls++
	return nil
}

func (sender *contractSlackSender) callCount() int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return sender.calls
}

func contractLocalDate(value time.Time, location *time.Location) domain.LocalDate {
	local := value.In(location)
	return domain.LocalDate{Year: local.Year(), Month: int(local.Month()), Day: local.Day()}
}

func runContractRunner(t *testing.T, runner *Runner) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	close(ready)
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, ready) }()
	return cancel, done
}

func stopContractRunner(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Runner.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Runner.Run() did not stop after context cancellation")
	}
}

func waitContractSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitContractTimerCount(t *testing.T, clock *contractClock, count int) {
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
