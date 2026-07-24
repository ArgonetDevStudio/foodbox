package ocr

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

const minimumInferConfidence = 0.6

var (
	dateInTextPattern = regexp.MustCompile(`\d{1,2}월\s*\d{1,2}일`)
	monthTokenPattern = regexp.MustCompile(`^\d{1,2}월$`)
	dayTokenPattern   = regexp.MustCompile(`^\d{1,2}일$`)
)

type inferTextField struct {
	y    int
	x    int
	text string
}

type dateTextField struct {
	x    int
	y    int
	text string
}

// Parse converts a Clova OCR response and its source image into menus. It only
// reads the image header, so memory use does not scale with decoded pixel data.
func Parse(response []byte, imageReader io.Reader, today domain.LocalDate) ([]domain.Menu, error) {
	var payload clovaResponse
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, fmt.Errorf("decode Clova OCR response: %w", err)
	}
	if len(payload.Images) == 0 {
		return nil, fmt.Errorf("decode Clova OCR response: images is empty")
	}
	config, _, err := image.DecodeConfig(imageReader)
	if err != nil {
		return nil, fmt.Errorf("decode menu image config: %w", err)
	}
	fields := payload.Images[0].Fields
	regions, err := calculateDayRegions(config.Width, config.Height, fields)
	if err != nil {
		return nil, err
	}
	return parseFields(fields, regions, today), nil
}

func ParseFile(response []byte, imagePath string, today domain.LocalDate) ([]domain.Menu, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("open menu image: %w", err)
	}
	defer file.Close()
	return Parse(response, file, today)
}

func parseFields(fields []clovaField, regions []DayRegion, today domain.LocalDate) []domain.Menu {
	menuFields := make([][]inferTextField, len(regions))
	dateFields := make([][]dateTextField, len(regions))
	for _, field := range fields {
		bounds, ok := field.bounds()
		if !ok || field.InferConfidence <= minimumInferConfidence || field.InferText == "" {
			continue
		}
		for index, region := range regions {
			if region.Date.Contains(bounds.centerX, bounds.centerY) {
				if isDateToken(field.InferText) {
					dateFields[index] = append(dateFields[index], dateTextField{x: bounds.centerX, y: bounds.centerY, text: field.InferText})
				}
				break
			}
			if region.Menu.Contains(bounds.centerX, bounds.centerY) {
				menuFields[index] = append(menuFields[index], inferTextField{y: bounds.centerY, x: bounds.centerX, text: field.InferText})
				break
			}
		}
	}

	menus := make([]domain.Menu, 0, len(regions))
	for index := range regions {
		dateText := mergeDateFields(dateFields[index], today.Month)
		if dateText == "" {
			continue
		}
		date, err := ParseEisoDate(dateText, today)
		if err != nil {
			continue
		}
		menuItems := buildMenuItems(menuFields[index])
		menus = append(menus, domain.NewMenu(date, menuItems))
	}
	return menus
}

func isDateToken(text string) bool {
	return dateInTextPattern.MatchString(text) ||
		monthTokenPattern.MatchString(text) ||
		dayTokenPattern.MatchString(text) ||
		isDayNumber(text)
}

func mergeDateFields(fields []dateTextField, currentMonth int) string {
	if len(fields) == 0 {
		return ""
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].x < fields[j].x })
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field.text)
	}
	merged := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	if isDayNumber(merged) {
		day, _ := strconv.Atoi(merged)
		return fmt.Sprintf("%02d월 %02d일", currentMonth, day)
	}
	return merged
}

func buildMenuItems(fields []inferTextField) []string {
	if len(fields) == 0 {
		return []string{}
	}
	// Clova fields are already in document order. Keeping that order matches the
	// Java parser and avoids reshuffling tokens that share a row.
	lines := make([]string, 0, len(fields))
	lastY := -1
	var line strings.Builder
	for _, field := range fields {
		if abs(field.y-lastY) > 10 {
			if text := strings.TrimSpace(line.String()); text != "" {
				lines = append(lines, text)
			}
			line.Reset()
		} else if line.Len() > 0 {
			line.WriteByte(' ')
		}
		line.WriteString(field.text)
		lastY = field.y
	}
	if text := strings.TrimSpace(line.String()); text != "" {
		lines = append(lines, text)
	}
	return lines
}
