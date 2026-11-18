package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"strings"
)

func CreditKey(name string) string {
	return textnorm.NormalizeForIdentity(strings.ReplaceAll(name, "$", "s"))
}

func Credits(credited []string, artist string) bool {
	want := CreditKey(artist)
	if want == "" {
		return false
	}
	for _, name := range credited {
		if CreditKey(name) == want {
			return true
		}
	}
	return false
}
