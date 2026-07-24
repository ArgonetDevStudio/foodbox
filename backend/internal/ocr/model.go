package ocr

type ParseRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (r ParseRegion) Contains(x, y int) bool {
	return r.X <= x && x <= r.X+r.Width && r.Y <= y && y <= r.Y+r.Height
}

type DayRegion struct {
	Date ParseRegion `json:"date"`
	Menu ParseRegion `json:"menu"`
}

type clovaResponse struct {
	Images []clovaImage `json:"images"`
}

type clovaImage struct {
	Fields []clovaField `json:"fields"`
}

type clovaField struct {
	BoundingPoly    boundingPoly `json:"boundingPoly"`
	InferText       string       `json:"inferText"`
	InferConfidence float64      `json:"inferConfidence"`
}

type boundingPoly struct {
	Vertices []vertex `json:"vertices"`
}

type vertex struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (f clovaField) bounds() (fieldBounds, bool) {
	if len(f.BoundingPoly.Vertices) < 4 {
		return fieldBounds{}, false
	}
	v := f.BoundingPoly.Vertices
	return fieldBounds{
		centerX: (int(v[0].X) + int(v[1].X) + int(v[2].X) + int(v[3].X)) / 4,
		centerY: (int(v[0].Y) + int(v[1].Y) + int(v[2].Y) + int(v[3].Y)) / 4,
		left:    min(int(v[0].X), int(v[3].X)),
		right:   max(int(v[1].X), int(v[2].X)),
		top:     min(int(v[0].Y), int(v[1].Y)),
		bottom:  max(int(v[2].Y), int(v[3].Y)),
	}, true
}

type fieldBounds struct {
	centerX int
	centerY int
	left    int
	right   int
	top     int
	bottom  int
}
