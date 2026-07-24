package ocr

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

type goldenMenu struct {
	Date  string   `json:"date"`
	Menus []string `json:"menus"`
	Valid bool     `json:"valid"`
}

func TestParseMatchesLegacyGoldenFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		image  string
		json   string
		golden string
		today  domain.LocalDate
	}{
		{name: "October 2025", image: "eiso_202510.jpg", json: "eiso_202510.json", golden: "eiso_202510.golden.json", today: domain.LocalDate{Year: 2025, Month: 10, Day: 1}},
		{name: "May 2026 numeric dates", image: "eiso_202605.png", json: "eiso_202605.json", golden: "eiso_202605.golden.json", today: domain.LocalDate{Year: 2026, Month: 5, Day: 1}},
		{name: "July 2026", image: "eiso_202607.jpg", json: "eiso_202607.json", golden: "eiso_202607.golden.json", today: domain.LocalDate{Year: 2026, Month: 7, Day: 1}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := readTestFile(t, filepath.Join(fixtureDirectory(), test.json))
			menus, err := ParseFile(response, filepath.Join(fixtureDirectory(), test.image), test.today)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}

			actual := make([]goldenMenu, 0, len(menus))
			for _, menu := range menus {
				actual = append(actual, goldenMenu{Date: menu.Date.String(), Menus: menu.Menus, Valid: menu.Valid})
			}
			var expected []goldenMenu
			if err := json.Unmarshal(readTestFile(t, filepath.Join("testdata", test.golden)), &expected); err != nil {
				t.Fatalf("decode golden file: %v", err)
			}
			if !reflect.DeepEqual(actual, expected) {
				actualJSON, _ := json.Marshal(actual)
				expectedJSON, _ := json.Marshal(expected)
				t.Fatalf("parsed menus differ from Java golden\nactual:   %s\nexpected: %s", actualJSON, expectedJSON)
			}
		})
	}
}

func TestCalculateDayRegionsMatchesLegacyBoundaries(t *testing.T) {
	t.Parallel()
	response := decodeResponse(t, filepath.Join(fixtureDirectory(), "eiso_202510.json"))
	regions, err := calculateDayRegions(800, 1168, response.Images[0].Fields)
	if err != nil {
		t.Fatalf("calculateDayRegions() error = %v", err)
	}

	columns := []ParseRegion{
		{X: 0, Width: 160},
		{X: 160, Width: 160},
		{X: 320, Width: 159},
		{X: 479, Width: 160},
		{X: 639, Width: 161},
	}
	rows := []struct {
		dateY, dateHeight int
		menuY, menuHeight int
	}{
		{177, 29, 206, 143},
		{346, 29, 375, 143},
		{515, 28, 543, 144},
		{683, 28, 711, 145},
		{852, 28, 880, 144},
	}
	want := make([]DayRegion, 0, 25)
	for _, row := range rows {
		for _, column := range columns {
			want = append(want, DayRegion{
				Date: ParseRegion{X: column.X, Y: row.dateY, Width: column.Width, Height: row.dateHeight},
				Menu: ParseRegion{X: column.X, Y: row.menuY, Width: column.Width, Height: row.menuHeight},
			})
		}
	}
	if !reflect.DeepEqual(regions, want) {
		t.Fatalf("regions differ\ngot:  %#v\nwant: %#v", regions, want)
	}
}

func TestParseFieldsConfidenceBoundaryAndDocumentOrder(t *testing.T) {
	t.Parallel()
	region := DayRegion{
		Date: ParseRegion{X: 0, Y: 0, Width: 100, Height: 20},
		Menu: ParseRegion{X: 0, Y: 21, Width: 100, Height: 100},
	}
	fields := []clovaField{
		fieldAt("07월 01일", 50, 10, 1),
		fieldAt("first", 50, 30, 0.9),
		fieldAt("ignored-at-threshold", 50, 45, minimumInferConfidence),
		fieldAt("second", 30, 60, 0.60001),
		fieldAt("token", 70, 60, 0.9),
	}
	menus := parseFields(fields, []DayRegion{region}, domain.LocalDate{Year: 2026, Month: 7, Day: 1})
	if len(menus) != 1 {
		t.Fatalf("len(menus) = %d, want 1", len(menus))
	}
	wantItems := []string{"first", "second token"}
	if !reflect.DeepEqual(menus[0].Menus, wantItems) {
		t.Fatalf("Menus = %#v, want %#v", menus[0].Menus, wantItems)
	}
}

func TestParseRejectsMalformedInputs(t *testing.T) {
	t.Parallel()
	image := readTestFile(t, filepath.Join(fixtureDirectory(), "eiso_202510.jpg"))
	tests := []struct {
		name     string
		response []byte
		image    []byte
	}{
		{name: "malformed JSON", response: []byte("{"), image: image},
		{name: "missing image result", response: []byte(`{"images":[]}`), image: image},
		{name: "invalid image", response: []byte(`{"images":[{"fields":[]}]}`), image: []byte("not an image")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.response, bytes.NewReader(test.image), domain.LocalDate{Year: 2025, Month: 10, Day: 1})
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
		})
	}
}

func fixtureDirectory() string {
	return "testdata"
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func decodeResponse(t *testing.T, path string) clovaResponse {
	t.Helper()
	var response clovaResponse
	if err := json.Unmarshal(readTestFile(t, path), &response); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return response
}

func fieldAt(text string, x, y int, confidence float64) clovaField {
	return clovaField{
		InferText:       text,
		InferConfidence: confidence,
		BoundingPoly: boundingPoly{Vertices: []vertex{
			{X: float64(x - 1), Y: float64(y - 1)},
			{X: float64(x + 1), Y: float64(y - 1)},
			{X: float64(x + 1), Y: float64(y + 1)},
			{X: float64(x - 1), Y: float64(y + 1)},
		}},
	}
}
