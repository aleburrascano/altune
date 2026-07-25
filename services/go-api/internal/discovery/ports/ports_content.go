package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type AlbumValidator interface {
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*AlbumValidationResult, error)
	ResolveArtistIdentity(ctx context.Context, artistName string) (*ArtistIdentity, error)
}

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
	FindRelatedByAlbum(ctx context.Context, album string, limit int) ([]RelatedTrackMatch, error)
	FindRelatedByArtist(ctx context.Context, artist string, limit int) ([]RelatedTrackMatch, error)
}
