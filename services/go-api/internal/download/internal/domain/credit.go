package domain

import (
	"altune/go-api/internal/shared/music"
	"strings"
)

func CreditKey(name string) string {
	return music.NormalizeForIdentity(strings.ReplaceAll(name, "$", "s"))
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
