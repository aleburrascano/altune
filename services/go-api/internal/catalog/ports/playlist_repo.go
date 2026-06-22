package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// PlaylistStore persists the Playlist aggregate itself: lifecycle, reads, and the
// preview-artwork projection.
type PlaylistStore interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
	GetPreviewArtwork(ctx context.Context, playlistId domain.PlaylistId) ([]string, error)
}

// PlaylistTrackMutator persists changes to a playlist's track membership. Separated
// from PlaylistStore so a consumer that only edits membership need not depend on the
// full aggregate surface (ISP).
type PlaylistTrackMutator interface {
	AddTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId, position int) error
	RemoveTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId) error
	ReorderTracks(ctx context.Context, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}

// PlaylistRepository is the full surface, composed of the two focused interfaces.
type PlaylistRepository interface {
	PlaylistStore
	PlaylistTrackMutator
}
