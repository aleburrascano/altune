package domain

import "altune/go-api/internal/shared/textnorm"

func OwnershipKey(title, artist string) string {
	return textnorm.NormalizeForMatch(title) + "|" + textnorm.NormalizeForMatch(artist)
}
