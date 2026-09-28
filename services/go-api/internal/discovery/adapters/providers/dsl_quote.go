package providers

func dslQuote(phrase string) string {
	return `"` + phrase + `"`
}
