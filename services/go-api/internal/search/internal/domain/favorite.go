package domain

import (
	"altune/go-api/internal/shared/music"
	"time"
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
		return music.NormalizeForMatch(title)
	}
	return music.NormalizeForMatch(subtitle) + "|" + music.NormalizeForMatch(title)
}

func FavoriteKeyOf(r SearchResult) string {
	return FavoriteKey(r.Kind, r.Title, r.Subtitle)
}

func ArtistKeyOf(r SearchResult) string {
	if r.Kind == ResultKindArtist {
		return music.NormalizeForMatch(r.Title)
	}
	return music.NormalizeForMatch(r.Subtitle)
}
