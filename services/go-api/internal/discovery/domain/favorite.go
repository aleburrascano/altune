package domain

import (
	"time"

	"altune/go-api/internal/shared/textnorm"
)

type Favorite struct {
	Kind      ResultKind
	Key       string
	Title     string
	Subtitle  string
	ImageURL  string
	CreatedAt time.Time
}

func FavoriteKey(kind ResultKind, title, subtitle string) string {
	if kind == ResultKindArtist {
		return textnorm.NormalizeForMatch(title)
	}
	return textnorm.NormalizeForMatch(subtitle) + "|" + textnorm.NormalizeForMatch(title)
}

func FavoriteKeyOf(r SearchResult) string {
	return FavoriteKey(r.Kind, r.Title, r.Subtitle)
}

func ArtistKeyOf(r SearchResult) string {
	if r.Kind == ResultKindArtist {
		return textnorm.NormalizeForMatch(r.Title)
	}
	return textnorm.NormalizeForMatch(r.Subtitle)
}
