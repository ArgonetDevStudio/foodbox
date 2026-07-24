package scheduler

import (
	"time"
	_ "time/tzdata"
)

const (
	seoulLocationName = "Asia/Seoul"
	defaultHour       = 9
)

// SeoulLocation returns a time zone that is available even in minimal images
// without an operating-system zoneinfo database.
func SeoulLocation() *time.Location {
	location, err := time.LoadLocation(seoulLocationName)
	if err != nil {
		panic("embedded Asia/Seoul time zone is unavailable: " + err.Error())
	}
	return location
}

// NextDailyRun returns the next occurrence of hour:minute in location.
// It constructs each candidate from calendar fields instead of adding 24 hours,
// so it remains correct when a location crosses a daylight-saving boundary.
func NextDailyRun(now time.Time, location *time.Location, hour, minute int) time.Time {
	if location == nil {
		location = SeoulLocation()
	}
	localNow := now.In(location)
	candidate := time.Date(
		localNow.Year(),
		localNow.Month(),
		localNow.Day(),
		hour,
		minute,
		0,
		0,
		location,
	)
	if !candidate.After(localNow) {
		tomorrow := localNow.AddDate(0, 0, 1)
		candidate = time.Date(
			tomorrow.Year(),
			tomorrow.Month(),
			tomorrow.Day(),
			hour,
			minute,
			0,
			0,
			location,
		)
	}
	return candidate
}
