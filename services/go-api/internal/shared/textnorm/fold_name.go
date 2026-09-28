package textnorm

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

func FoldName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(s)), " "))
}
