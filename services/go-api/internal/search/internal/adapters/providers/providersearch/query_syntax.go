package providersearch

import "strings"

const mbLuceneSpecialChars = `+-&|!(){}[]^"~*?:\/`

func MbLuceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(mbLuceneSpecialChars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func DslQuote(phrase string) string {
	return `"` + phrase + `"`
}

func ParseListeners(s string) int64 {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
}
