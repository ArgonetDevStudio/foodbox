package domain

// Menu represents one day's menu. Valid mirrors the legacy database field;
// use NewMenu for newly parsed menus so the validation rule is applied.
type Menu struct {
	Date  LocalDate `json:"date"`
	Menus []string  `json:"menus"`
	Valid bool      `json:"valid"`
}

func NewMenu(date LocalDate, menus []string) Menu {
	copied := append([]string(nil), menus...)
	return Menu{
		Date:  date,
		Menus: copied,
		Valid: len(copied) > 2,
	}
}

func (m Menu) Clone() Menu {
	m.Menus = append([]string(nil), m.Menus...)
	return m
}
