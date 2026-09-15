package eval

// quoteJSON renders an outcome enum's String() label as a JSON string. Labels
// are fixed lower-case identifiers, so no escaping is needed.
func quoteJSON(s string) ([]byte, error) {
	return []byte(`"` + s + `"`), nil
}
