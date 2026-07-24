package ocr

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

var (
	fullDatePattern  = regexp.MustCompile(`^\d{1,2}월\s*\d{1,2}일$`)
	datePartPattern  = regexp.MustCompile(`^\d{1,2}(월|일)$`)
	dayNumberPattern = regexp.MustCompile(`^\d{1,2}$`)
	weekdayPattern   = regexp.MustCompile(`^(월요일|화요일|수요일|목요일|금요일)$`)
)

type weekdayInfo struct {
	centerX int
	right   int
	bottom  int
}

type columnInfo struct {
	startX int
	width  int
}

type dateInfo struct {
	y      int
	top    int
	bottom int
}

type dateRow struct {
	y     int
	dates []dateInfo
}

func calculateDayRegions(imageWidth, imageHeight int, fields []clovaField) ([]DayRegion, error) {
	weekdays := collectWeekdays(fields)
	if len(weekdays) == 0 {
		return nil, fmt.Errorf("calculate OCR regions: no weekday headings found")
	}
	columns := calculateColumns(weekdays, imageWidth)
	dates := collectDates(fields, weekdays)
	rows := groupDatesByRow(dates)
	if len(rows) == 0 {
		return nil, fmt.Errorf("calculate OCR regions: no date rows found")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].y < rows[j].y })

	regions := make([]DayRegion, 0, len(rows)*len(columns))
	observedMenuHeights := make([]int, 0, len(rows)-1)
	for rowIndex, row := range rows {
		dateTop, dateBottom := row.dates[0].top, row.dates[0].bottom
		for _, date := range row.dates[1:] {
			dateTop = min(dateTop, date.top)
			dateBottom = max(dateBottom, date.bottom)
		}

		nextRowY := imageHeight
		if rowIndex < len(rows)-1 {
			nextRowY = rows[rowIndex+1].y
		}
		menuStartY := dateBottom + 5
		menuHeightCandidate := nextRowY - menuStartY - 10
		menuHeight := menuHeightCandidate
		if rowIndex == len(rows)-1 && len(observedMenuHeights) > 0 {
			menuHeight = min(menuHeightCandidate, roundedAverage(observedMenuHeights))
		}
		menuHeight = max(menuHeight, 20)
		if rowIndex < len(rows)-1 {
			observedMenuHeights = append(observedMenuHeights, menuHeight)
		}

		for _, column := range columns {
			regions = append(regions, DayRegion{
				Date: ParseRegion{X: column.startX, Y: dateTop - 5, Width: column.width, Height: dateBottom - dateTop + 10},
				Menu: ParseRegion{X: column.startX, Y: menuStartY, Width: column.width, Height: menuHeight},
			})
		}
	}
	return regions, nil
}

func collectWeekdays(fields []clovaField) []weekdayInfo {
	result := make([]weekdayInfo, 0, 5)
	for _, field := range fields {
		if !weekdayPattern.MatchString(field.InferText) {
			continue
		}
		bounds, ok := field.bounds()
		if !ok {
			continue
		}
		result = append(result, weekdayInfo{centerX: bounds.centerX, right: bounds.right, bottom: bounds.bottom})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].centerX < result[j].centerX })
	return result
}

func calculateColumns(weekdays []weekdayInfo, imageWidth int) []columnInfo {
	columns := make([]columnInfo, 0, len(weekdays))
	for i, weekday := range weekdays {
		var startX, width int
		switch {
		case i == 0:
			if len(weekdays) > 1 {
				width = (weekdays[i+1].centerX + weekday.centerX) / 2
			} else {
				width = weekday.right
			}
		case i == len(weekdays)-1:
			startX = (weekdays[i-1].centerX + weekday.centerX) / 2
			width = imageWidth - startX
		default:
			startX = (weekdays[i-1].centerX + weekday.centerX) / 2
			width = (weekdays[i+1].centerX+weekday.centerX)/2 - startX
		}
		columns = append(columns, columnInfo{startX: startX, width: width})
	}
	return columns
}

func collectDates(fields []clovaField, weekdays []weekdayInfo) []dateInfo {
	weekdayBottom := 0
	for _, weekday := range weekdays {
		weekdayBottom = max(weekdayBottom, weekday.bottom)
	}
	dates := make([]dateInfo, 0, 25)
	numericDates := make([]dateInfo, 0, 25)
	for _, field := range fields {
		bounds, ok := field.bounds()
		if !ok || bounds.centerY <= weekdayBottom {
			continue
		}
		date := dateInfo{y: bounds.centerY, top: bounds.top, bottom: bounds.bottom}
		if fullDatePattern.MatchString(field.InferText) || datePartPattern.MatchString(field.InferText) {
			dates = append(dates, date)
			continue
		}
		if isDayNumber(field.InferText) {
			numericDates = append(numericDates, date)
		}
	}
	if len(dates) < 3 {
		return append(dates, numericDates...)
	}
	firstDateRowY := dates[0].y
	for _, date := range dates[1:] {
		firstDateRowY = min(firstDateRowY, date.y)
	}
	for _, date := range numericDates {
		if date.y < firstDateRowY {
			dates = append(dates, date)
		}
	}
	return dates
}

func groupDatesByRow(dates []dateInfo) []dateRow {
	rows := make([]dateRow, 0, 5)
	for _, date := range dates {
		rowIndex := -1
		for i := range rows {
			if abs(rows[i].y-date.y) < 30 {
				rowIndex = i
				break
			}
		}
		if rowIndex < 0 {
			rows = append(rows, dateRow{y: date.y, dates: []dateInfo{date}})
			continue
		}
		rows[rowIndex].dates = append(rows[rowIndex].dates, date)
	}
	return rows
}

func isDayNumber(text string) bool {
	if !dayNumberPattern.MatchString(text) {
		return false
	}
	day, err := strconv.Atoi(text)
	return err == nil && 1 <= day && day <= 31
}

func roundedAverage(numbers []int) int {
	sum := 0
	for _, number := range numbers {
		sum += number
	}
	// All inputs are non-negative. This matches Java Math.round(double).
	return (2*sum + len(numbers)) / (2 * len(numbers))
}

func abs(number int) int {
	if number < 0 {
		return -number
	}
	return number
}
