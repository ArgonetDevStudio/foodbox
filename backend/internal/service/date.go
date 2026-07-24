package service

import (
	"fmt"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

var seoul = time.FixedZone("Asia/Seoul", 9*60*60)

func todayInSeoul() domain.LocalDate {
	now := time.Now().In(seoul)
	return domain.LocalDate{Year: now.Year(), Month: int(now.Month()), Day: now.Day()}
}

func asTime(date domain.LocalDate) time.Time {
	return time.Date(date.Year, time.Month(date.Month), date.Day, 0, 0, 0, 0, seoul)
}

func isWeekend(date domain.LocalDate) bool {
	weekday := asTime(date).Weekday()
	return weekday == time.Saturday || weekday == time.Sunday
}

func compareDate(left, right domain.LocalDate) int {
	switch {
	case left.Year != right.Year:
		if left.Year < right.Year {
			return -1
		}
		return 1
	case left.Month != right.Month:
		if left.Month < right.Month {
			return -1
		}
		return 1
	case left.Day < right.Day:
		return -1
	case left.Day > right.Day:
		return 1
	default:
		return 0
	}
}

func formatDate(date domain.LocalDate) string {
	return fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
}

func koreanWeekday(date domain.LocalDate) string {
	return [...]string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}[asTime(date).Weekday()]
}

func isLastWednesday(date domain.LocalDate) bool {
	if asTime(date).Weekday() != time.Wednesday {
		return false
	}
	lastDay := time.Date(date.Year, time.Month(date.Month)+1, 0, 0, 0, 0, 0, seoul).Day()
	return lastDay-date.Day < 7
}
