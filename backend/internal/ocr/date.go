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
	current, currentValid := strictDate(today.Year, month, day)
	if currentValid && dayDifference(current, todayTime) <= 45 {
		return localDate(current), nil
	}

	candidates := make([]time.Time, 0, 3)
	if currentValid {
		candidates = append(candidates, current)
	}
	if previous, valid := strictDate(today.Year-1, month, day); valid {
		candidates = append(candidates, previous)
	}
	if next, valid := strictDate(today.Year+1, month, day); valid {
		candidates = append(candidates, next)
	}
	if len(candidates) == 0 {
		return domain.LocalDate{}, fmt.Errorf("invalid Eisodosirak date in adjacent years: %d월 %d일", month, day)
	}

	closest := candidates[0]
	closestDifference := dayDifference(closest, todayTime)
	for _, candidate := range candidates[1:] {
		if difference := dayDifference(candidate, todayTime); difference < closestDifference {
			closest = candidate
			closestDifference = difference
		}
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
