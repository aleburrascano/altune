package eval

func quoteJSON(s string) ([]byte, error) {
	return []byte(`"` + s + `"`), nil
}
