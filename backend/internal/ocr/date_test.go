package ocr

import (
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

func TestParseEisoDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		today domain.LocalDate
		want  domain.LocalDate
	}{
		{name: "spaced", value: "09월 29일", today: domain.LocalDate{Year: 2025, Month: 10, Day: 1}, want: domain.LocalDate{Year: 2025, Month: 9, Day: 29}},
		{name: "compact", value: "10월21일", today: domain.LocalDate{Year: 2025, Month: 10, Day: 1}, want: domain.LocalDate{Year: 2025, Month: 10, Day: 21}},
		{name: "OCR typo", value: " 12윌 31일 ", today: domain.LocalDate{Year: 2026, Month: 1, Day: 10}, want: domain.LocalDate{Year: 2025, Month: 12, Day: 31}},
		{name: "closest next year", value: "01월 03일", today: domain.LocalDate{Year: 2026, Month: 12, Day: 20}, want: domain.LocalDate{Year: 2027, Month: 1, Day: 3}},
		{name: "current year within 45 days", value: "11월 15일", today: domain.LocalDate{Year: 2026, Month: 12, Day: 30}, want: domain.LocalDate{Year: 2026, Month: 11, Day: 15}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseEisoDate(test.value, test.today)
			if err != nil {
				t.Fatalf("ParseEisoDate() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseEisoDate() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseEisoDateRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"2026-07-01", "13월 1일", "02월 30일"} {
		if _, err := ParseEisoDate(value, domain.LocalDate{Year: 2026, Month: 7, Day: 1}); err == nil {
			t.Errorf("ParseEisoDate(%q) error = nil, want error", value)
		}
	}
}

func TestParseEisoDateLeapDayUsesOnlyValidAdjacentYearCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		today domain.LocalDate
		want  domain.LocalDate
	}{
		{
			name:  "current leap year remains the only valid candidate",
			today: domain.LocalDate{Year: 2024, Month: 12, Day: 31},
			want:  domain.LocalDate{Year: 2024, Month: 2, Day: 29},
		},
		{
			name:  "previous leap year from non-leap current year",
			today: domain.LocalDate{Year: 2025, Month: 1, Day: 10},
			want:  domain.LocalDate{Year: 2024, Month: 2, Day: 29},
		},
		{
			name:  "next leap year from non-leap current year",
			today: domain.LocalDate{Year: 2027, Month: 12, Day: 20},
			want:  domain.LocalDate{Year: 2028, Month: 2, Day: 29},
		},
		{
			name:  "current leap year within 45 days",
			today: domain.LocalDate{Year: 2024, Month: 3, Day: 30},
			want:  domain.LocalDate{Year: 2024, Month: 2, Day: 29},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseEisoDate("02월 29일", test.today)
			if err != nil {
				t.Fatalf("ParseEisoDate() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseEisoDate() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseEisoDateLeapDayRejectsWhenAdjacentYearsAreNotLeapYears(t *testing.T) {
	t.Parallel()
	_, err := ParseEisoDate("02월 29일", domain.LocalDate{Year: 2026, Month: 7, Day: 1})
	if err == nil {
		t.Fatal("ParseEisoDate() error = nil, want error when previous, current, and next years are non-leap")
	}
}
