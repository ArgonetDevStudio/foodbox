package scheduler

import (
	"testing"
	"time"
)

func TestNextDailyRunBeforeNine(t *testing.T) {
	location := SeoulLocation()
	now := time.Date(2026, time.July, 25, 8, 30, 0, 0, location)

	got := NextDailyRun(now, location, 9, 0)
	want := time.Date(2026, time.July, 25, 9, 0, 0, 0, location)

	if !got.Equal(want) {
		t.Fatalf("NextDailyRun() = %v, want %v", got, want)
	}
}

func TestNextDailyRunAtNineUsesTomorrow(t *testing.T) {
	location := SeoulLocation()
	now := time.Date(2026, time.July, 25, 9, 0, 0, 0, location)

	got := NextDailyRun(now, location, 9, 0)
	want := time.Date(2026, time.July, 26, 9, 0, 0, 0, location)

	if !got.Equal(want) {
		t.Fatalf("NextDailyRun() = %v, want %v", got, want)
	}
}

func TestNextDailyRunUsesCalendarDayAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.March, 7, 10, 0, 0, 0, location)

	got := NextDailyRun(now, location, 9, 0)
	want := time.Date(2026, time.March, 8, 9, 0, 0, 0, location)

	if !got.Equal(want) {
		t.Fatalf("NextDailyRun() = %v, want %v", got, want)
	}
	if got.Sub(now) != 22*time.Hour {
		t.Fatalf("duration across DST = %v, want 22h", got.Sub(now))
	}
}
