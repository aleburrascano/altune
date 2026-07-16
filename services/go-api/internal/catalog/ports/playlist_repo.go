package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// PlaylistRepository persists the Playlist aggregate: lifecycle, reads (with
// their read-side projections — track count and preview artwork ride on
// ListForUser), and track membership. One flat surface: every consumer today
// uses all of it; carve a narrower interface at the consumer the day one wants
// less (Go interfaces are satisfied implicitly, so that move costs nothing).
type PlaylistRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
	AddTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId, position int) error
	RemoveTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId) error
	ReorderTracks(ctx context.Context, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
