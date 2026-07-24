package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const isoDateLayout = "2006-01-02"

// LocalDate represents a calendar date without a time or timezone.
// Its JSON representation is the legacy Spring/Jackson [year, month, day]
// array so existing database files remain compatible.
type LocalDate struct {
	Year  int
	Month int
	Day   int
}

func NewLocalDate(year, month, day int) (LocalDate, error) {
	date := LocalDate{Year: year, Month: month, Day: day}
	if err := date.Validate(); err != nil {
		return LocalDate{}, err
	}
	return date, nil
}

func ParseLocalDate(value string) (LocalDate, error) {
	parsed, err := time.Parse(isoDateLayout, value)
	if err != nil {
		return LocalDate{}, fmt.Errorf("parse local date %q: %w", value, err)
	}
	return FromTime(parsed), nil
}

func FromTime(value time.Time) LocalDate {
	return LocalDate{
		Year:  value.Year(),
		Month: int(value.Month()),
		Day:   value.Day(),
	}
}

func TodayIn(location *time.Location) LocalDate {
	if location == nil {
		location = time.UTC
	}
	return FromTime(time.Now().In(location))
}

func (d LocalDate) Validate() error {
	if d.Year < 1 || d.Year > 9999 {
		return fmt.Errorf("year must be between 1 and 9999: %d", d.Year)
	}
	if d.Month < 1 || d.Month > 12 {
		return fmt.Errorf("month must be between 1 and 12: %d", d.Month)
	}
	if d.Day < 1 || d.Day > 31 {
		return fmt.Errorf("day must be between 1 and 31: %d", d.Day)
	}

	converted := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
	if converted.Year() != d.Year || int(converted.Month()) != d.Month || converted.Day() != d.Day {
		return fmt.Errorf("invalid calendar date: %s", d.uncheckedString())
	}
	return nil
}

func (d LocalDate) Time(location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, location)
}

func (d LocalDate) String() string {
	if err := d.Validate(); err != nil {
		return "invalid-date"
	}
	return d.uncheckedString()
}

func (d LocalDate) uncheckedString() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func (d LocalDate) Compare(other LocalDate) int {
	if d.Year != other.Year {
		return compareInt(d.Year, other.Year)
	}
	if d.Month != other.Month {
		return compareInt(d.Month, other.Month)
	}
	return compareInt(d.Day, other.Day)
}

func (d LocalDate) Before(other LocalDate) bool {
	return d.Compare(other) < 0
}

func (d LocalDate) After(other LocalDate) bool {
	return d.Compare(other) > 0
}

func (d LocalDate) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("marshal local date: %w", err)
	}
	return []byte("[" + strconv.Itoa(d.Year) + "," + strconv.Itoa(d.Month) + "," + strconv.Itoa(d.Day) + "]"), nil
}

func (d *LocalDate) UnmarshalJSON(data []byte) error {
	if d == nil {
		return fmt.Errorf("unmarshal local date into nil receiver")
	}

	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("unmarshal local date: empty JSON value")
	}

	var parsed LocalDate
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("unmarshal ISO local date: %w", err)
		}
		var err error
		parsed, err = ParseLocalDate(value)
		if err != nil {
			return err
		}
	} else {
		var values []int
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("unmarshal legacy local date: %w", err)
		}
		if len(values) != 3 {
			return fmt.Errorf("legacy local date must contain exactly 3 integers, got %d", len(values))
		}
		parsed = LocalDate{Year: values[0], Month: values[1], Day: values[2]}
		if err := parsed.Validate(); err != nil {
			return fmt.Errorf("unmarshal legacy local date: %w", err)
		}
	}

	*d = parsed
	return nil
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
