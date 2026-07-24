package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLocalDateJSONCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  LocalDate
	}{
		{name: "legacy array", input: `[2026,7,25]`, want: LocalDate{Year: 2026, Month: 7, Day: 25}},
		{name: "ISO string", input: `"2026-07-25"`, want: LocalDate{Year: 2026, Month: 7, Day: 25}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var got LocalDate
			if err := json.Unmarshal([]byte(test.input), &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Unmarshal() = %+v, want %+v", got, test.want)
			}

			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(encoded) != `[2026,7,25]` {
				t.Fatalf("Marshal() = %s, want legacy array", encoded)
			}
		})
	}
}

func TestLocalDateRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	inputs := []string{
		`[2026,2,29]`,
		`[2026,7]`,
		`[2026,7,25,1]`,
		`"2026-02-29"`,
		`null`,
		`{}`,
	}
	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var date LocalDate
			if err := json.Unmarshal([]byte(input), &date); err == nil {
				t.Fatalf("Unmarshal(%s) unexpectedly succeeded: %+v", input, date)
			}
		})
	}
}

func TestLocalDateOrderingAndTime(t *testing.T) {
	t.Parallel()

	earlier := LocalDate{Year: 2025, Month: 12, Day: 31}
	later := LocalDate{Year: 2026, Month: 1, Day: 1}
	if !earlier.Before(later) || earlier.After(later) || later.Compare(earlier) != 1 {
		t.Fatal("date ordering is incorrect")
	}

	location := time.FixedZone("test", 9*60*60)
	got := later.Time(location)
	if got.Year() != 2026 || got.Month() != time.January || got.Day() != 1 || got.Location() != location {
		t.Fatalf("Time() = %v", got)
	}
}

func TestNewMenuCopiesInputAndComputesValidity(t *testing.T) {
	t.Parallel()

	items := []string{"one", "two", "three"}
	menu := NewMenu(LocalDate{Year: 2026, Month: 7, Day: 25}, items)
	items[0] = "changed"
	if !menu.Valid {
		t.Fatal("NewMenu() should mark 3 items as valid")
	}
	if menu.Menus[0] != "one" {
		t.Fatal("NewMenu() retained the caller's mutable slice")
	}
}
