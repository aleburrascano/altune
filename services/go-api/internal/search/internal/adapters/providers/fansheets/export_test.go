package fansheets

func ParseSheet(body []byte) (any, error) {
	rows, mapped, err := parseSheet(body)
	return struct {
		Rows   []trackerRow
		Mapped bool
	}{rows, mapped}, err
}
