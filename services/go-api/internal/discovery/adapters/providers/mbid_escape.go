package providers

import "strings"

const mbLuceneSpecialChars = `+-&|!(){}[]^"~*?:\/`

func mbLuceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(mbLuceneSpecialChars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
