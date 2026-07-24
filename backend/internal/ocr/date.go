package ocr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

var eisoDatePattern = regexp.MustCompile(`(\d{1,2})월\s*(\d{1,2})일`)

func ParseEisoDate(value string, today domain.LocalDate) (domain.LocalDate, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "윌", "월"))
	match := eisoDatePattern.FindStringSubmatch(value)
	if match == nil {
		return domain.LocalDate{}, fmt.Errorf("invalid Eisodosirak date: %q", value)
	}
	month, _ := strconv.Atoi(match[1])
	day, _ := strconv.Atoi(match[2])
	return resolveClosestDate(today, month, day)
}

func resolveClosestDate(today domain.LocalDate, month, day int) (domain.LocalDate, error) {
	todayTime := time.Date(today.Year, time.Month(today.Month), today.Day, 0, 0, 0, 0, time.UTC)
	current, ok := strictDate(today.Year, month, day)
	if !ok {
		return domain.LocalDate{}, fmt.Errorf("invalid Eisodosirak date: %d월 %d일", month, day)
	}
	if dayDifference(current, todayTime) <= 45 {
		return localDate(current), nil
	}
	previous, _ := strictDate(today.Year-1, month, day)
	next, _ := strictDate(today.Year+1, month, day)
	closest := current
	closestDifference := dayDifference(current, todayTime)
	if difference := dayDifference(previous, todayTime); difference < closestDifference {
		closest = previous
		closestDifference = difference
	}
	if difference := dayDifference(next, todayTime); difference < closestDifference {
		closest = next
	}
	return localDate(closest), nil
}

func strictDate(year, month, day int) (time.Time, bool) {
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return date, date.Year() == year && int(date.Month()) == month && date.Day() == day
}

func dayDifference(first, second time.Time) int {
	difference := int(first.Sub(second).Hours() / 24)
	return abs(difference)
}

func localDate(date time.Time) domain.LocalDate {
	return domain.LocalDate{Year: date.Year(), Month: int(date.Month()), Day: date.Day()}
}
