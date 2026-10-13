package domain

import (
	"strings"
	"unicode/utf8"
)

func IsStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
