package ports

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
)

type ArtistIdentityResolver interface {
	ResolveArtistIdentity(ctx context.Context, artistName string) (*ArtistIdentity, error)
}

type ArtistIdentity struct {
	MBID           string
	Disambiguation string
	BirthYear      int
	Area           string
	ArtistType     string
}

type AlbumValidationResult struct {
	Confirmed   []domain.SearchResult
	Unconfirmed []domain.SearchResult
	ArtistMBID  string
}

type RelatedTrackMatch struct {
	Title      string
	Artist     string
	Album      string
	ArtworkURL *string
}

type DiscogsArtistInfo struct {
	ID      int
	Name    string
	Genre   string
	Country string
	Overlap int
}

type DiscogsRelease struct {
	Title string
	Year  int
	Type  string
}

type RelationshipQuerier interface {
	FindRelatedByAlbum(ctx context.Context, userId shared.UserId, album string, limit int) ([]RelatedTrackMatch, error)
}
