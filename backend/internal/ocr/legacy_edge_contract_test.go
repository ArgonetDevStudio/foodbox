package ocr

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

func TestLegacyParseRegionIncludesEveryBoundary(t *testing.T) {
	t.Parallel()
	region := ParseRegion{X: 10, Y: 20, Width: 30, Height: 40}
	tests := []struct {
		name string
		x    int
		y    int
		want bool
	}{
		{name: "top left", x: 10, y: 20, want: true},
		{name: "top right", x: 40, y: 20, want: true},
		{name: "bottom left", x: 10, y: 60, want: true},
		{name: "bottom right", x: 40, y: 60, want: true},
		{name: "before left", x: 9, y: 20, want: false},
		{name: "after right", x: 41, y: 20, want: false},
		{name: "above top", x: 10, y: 19, want: false},
		{name: "below bottom", x: 10, y: 61, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := region.Contains(test.x, test.y); got != test.want {
				t.Fatalf("Contains(%d, %d) = %t, want %t", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestLegacyConfidenceThresholdIsStrict(t *testing.T) {
	t.Parallel()
	region := DayRegion{
		Date: ParseRegion{X: 0, Y: 0, Width: 100, Height: 20},
		Menu: ParseRegion{X: 0, Y: 21, Width: 100, Height: 100},
	}
	fields := []clovaField{
		fieldAt("07월 01일", 100, 20, 1),
		fieldAt("excluded", 50, 30, 0.6),
		fieldAt("included", 100, 121, 0.6000001),
	}

	menus := parseFields(fields, []DayRegion{region}, domain.LocalDate{Year: 2026, Month: 7, Day: 1})
	if len(menus) != 1 {
		t.Fatalf("len(menus) = %d, want 1", len(menus))
	}
	if want := []string{"included"}; !reflect.DeepEqual(menus[0].Menus, want) {
		t.Fatalf("Menus = %#v, want %#v", menus[0].Menus, want)
	}
}

func TestLegacyDateRowClusteringThreshold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		delta int
		want  int
	}{
		{name: "29 pixels remains in one row", delta: 29, want: 1},
		{name: "30 pixels starts another row", delta: 30, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := groupDatesByRow([]dateInfo{{y: 100}, {y: 100 + test.delta}})
			if len(rows) != test.want {
				t.Fatalf("len(groupDatesByRow(delta=%d)) = %d, want %d", test.delta, len(rows), test.want)
			}
		})
	}
}

func TestLegacyMenuLineThresholdPreservesDocumentOrder(t *testing.T) {
	t.Parallel()
	fields := []inferTextField{
		{x: 90, y: 100, text: "first"},
		{x: 10, y: 110, text: "second"},
		{x: 50, y: 121, text: "third"},
	}
	want := []string{"first second", "third"}
	if got := buildMenuItems(fields); !reflect.DeepEqual(got, want) {
		t.Fatalf("buildMenuItems() = %#v, want %#v", got, want)
	}
}

func TestLegacySplitDateTokensAreSortedByX(t *testing.T) {
	t.Parallel()
	region := DayRegion{
		Date: ParseRegion{X: 0, Y: 0, Width: 100, Height: 20},
		Menu: ParseRegion{X: 0, Y: 21, Width: 100, Height: 100},
	}
	fields := []clovaField{
		fieldAt("07일", 70, 10, 1),
		fieldAt("10월", 20, 10, 1),
		fieldAt("menu", 50, 30, 1),
	}

	menus := parseFields(fields, []DayRegion{region}, domain.LocalDate{Year: 2025, Month: 10, Day: 1})
	if len(menus) != 1 {
		t.Fatalf("len(menus) = %d, want 1", len(menus))
	}
	if want := (domain.LocalDate{Year: 2025, Month: 10, Day: 7}); menus[0].Date != want {
		t.Fatalf("Date = %v, want %v", menus[0].Date, want)
	}
}

func TestLegacyInvalidAndWeekdayOnlyCellsAreSkipped(t *testing.T) {
	t.Parallel()
	regions := []DayRegion{
		{Date: ParseRegion{X: 0, Y: 0, Width: 99, Height: 20}, Menu: ParseRegion{X: 0, Y: 21, Width: 99, Height: 100}},
		{Date: ParseRegion{X: 100, Y: 0, Width: 99, Height: 20}, Menu: ParseRegion{X: 100, Y: 21, Width: 99, Height: 100}},
		{Date: ParseRegion{X: 200, Y: 0, Width: 100, Height: 20}, Menu: ParseRegion{X: 200, Y: 21, Width: 100, Height: 100}},
	}
	fields := []clovaField{
		fieldAt("13월 40일", 50, 10, 1),
		fieldAt("invalid date menu", 50, 30, 1),
		fieldAt("월요일", 150, 10, 1),
		fieldAt("weekday-only menu", 150, 30, 1),
		fieldAt("10월 08일", 250, 10, 1),
		fieldAt("kept", 250, 30, 1),
	}

	menus := parseFields(fields, regions, domain.LocalDate{Year: 2025, Month: 10, Day: 1})
	if len(menus) != 1 {
		t.Fatalf("len(menus) = %d, want 1; menus = %#v", len(menus), menus)
	}
	if want := (domain.LocalDate{Year: 2025, Month: 10, Day: 8}); menus[0].Date != want {
		t.Fatalf("Date = %v, want %v", menus[0].Date, want)
	}
}

func TestLegacyDateInferenceEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		today domain.LocalDate
		want  domain.LocalDate
	}{
		{name: "OCR month typo", value: "12윌 31일", today: domain.LocalDate{Year: 2026, Month: 1, Day: 10}, want: domain.LocalDate{Year: 2025, Month: 12, Day: 31}},
		{name: "year end chooses next January", value: "01월 03일", today: domain.LocalDate{Year: 2026, Month: 12, Day: 20}, want: domain.LocalDate{Year: 2027, Month: 1, Day: 3}},
		{name: "new year chooses previous December", value: "12월 31일", today: domain.LocalDate{Year: 2026, Month: 1, Day: 10}, want: domain.LocalDate{Year: 2025, Month: 12, Day: 31}},
		{name: "current year at inclusive 45 day boundary", value: "11월 15일", today: domain.LocalDate{Year: 2026, Month: 12, Day: 30}, want: domain.LocalDate{Year: 2026, Month: 11, Day: 15}},
		{name: "current leap year", value: "02월 29일", today: domain.LocalDate{Year: 2024, Month: 3, Day: 30}, want: domain.LocalDate{Year: 2024, Month: 2, Day: 29}},
		{name: "previous leap year", value: "02월 29일", today: domain.LocalDate{Year: 2025, Month: 1, Day: 10}, want: domain.LocalDate{Year: 2024, Month: 2, Day: 29}},
		{name: "next leap year", value: "02월 29일", today: domain.LocalDate{Year: 2027, Month: 12, Day: 20}, want: domain.LocalDate{Year: 2028, Month: 2, Day: 29}},
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

	if _, err := ParseEisoDate("02월 29일", domain.LocalDate{Year: 2026, Month: 7, Day: 1}); err == nil {
		t.Fatal("ParseEisoDate() error = nil, want error when all adjacent years are non-leap")
	}
}

func TestLegacyFixtureBytesAndExactOutputs(t *testing.T) {
	t.Parallel()
	type expectedMenu struct {
		Date  string   `json:"date"`
		Menus []string `json:"menus"`
		Valid bool     `json:"valid"`
	}
	tests := []struct {
		name         string
		image        string
		imageHash    string
		response     string
		responseHash string
		golden       string
		goldenHash   string
		today        domain.LocalDate
	}{
		{name: "October 2025", image: "eiso_202510.jpg", imageHash: "5e4b3da46e4bdf9163d95cd72131834b67bd702276a6c289ef1e669af6dcd5f1", response: "eiso_202510.json", responseHash: "2a21f7160bb36c8b26da77e063ad607cc51233e80b1b556ea6810d4b6f279ca7", golden: "eiso_202510.golden.json", goldenHash: "7b3d53182bfdb49cef3ab82a635a55f4dd901abbf5c2fc882db7d462b18ec18c", today: domain.LocalDate{Year: 2025, Month: 10, Day: 1}},
		{name: "May 2026", image: "eiso_202605.png", imageHash: "82f04cef1617c89af671a552cb282352af5c4ec8f6a999afbcbe508f6c227dbb", response: "eiso_202605.json", responseHash: "bc60a2a0049128f563c610a6da30a8f7df793903974fb43896d11e10b992c806", golden: "eiso_202605.golden.json", goldenHash: "2d33795934c7cf355b3f66a22b755e756ec88d099bf1bc05a24cb48bbc45714b", today: domain.LocalDate{Year: 2026, Month: 5, Day: 1}},
		{name: "July 2026", image: "eiso_202607.jpg", imageHash: "3d66a4519421f8c95a36d381aa6425ccab602d044f2a1ce2e70bfb6f1cfbda78", response: "eiso_202607.json", responseHash: "95e1065a0251363fecfdd795cb6c3c6b9d01c9d999367cb5e7aa54dbe5ae79d2", golden: "eiso_202607.golden.json", goldenHash: "53b316845238765d69d8bb4942c0dc03c0006a54f9cc5b211c63d34acbdda1f6", today: domain.LocalDate{Year: 2026, Month: 7, Day: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			imagePath := filepath.Join(fixtureDirectory(), test.image)
			responsePath := filepath.Join(fixtureDirectory(), test.response)
			goldenPath := filepath.Join(fixtureDirectory(), test.golden)
			assertLegacyFixtureHash(t, imagePath, test.imageHash)
			assertLegacyFixtureHash(t, responsePath, test.responseHash)
			assertLegacyFixtureHash(t, goldenPath, test.goldenHash)

			menus, err := ParseFile(readTestFile(t, responsePath), imagePath, test.today)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			actual := make([]expectedMenu, 0, len(menus))
			for _, menu := range menus {
				actual = append(actual, expectedMenu{Date: menu.Date.String(), Menus: menu.Menus, Valid: menu.Valid})
			}
			var expected []expectedMenu
			if err := json.Unmarshal(readTestFile(t, goldenPath), &expected); err != nil {
				t.Fatalf("decode golden file: %v", err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("parsed output differs from pinned legacy golden\nactual: %#v\nexpected: %#v", actual, expected)
			}
		})
	}
}

func assertLegacyFixtureHash(t *testing.T, path, want string) {
	t.Helper()
	digest := sha256.Sum256(readTestFile(t, path))
	if got := fmt.Sprintf("%x", digest); got != want {
		t.Fatalf("SHA-256(%s) = %s, want %s", path, got, want)
	}
}
