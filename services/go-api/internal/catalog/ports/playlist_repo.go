package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

// ErrPlaylistNotOwned is returned by a PlaylistMembershipRepository write when
// the playlist does not exist or is not owned by the supplied user. The write is
// refused atomically at the data layer, so no membership row is touched.
var ErrPlaylistNotOwned = errors.New("playlist not found for user")

// PlaylistLifecycleRepository persists playlists and their metadata.
type PlaylistLifecycleRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]domain.PlaylistWithSummary, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
}

// PlaylistMembershipRepository manages the tracks contained within a playlist.
// Every write is owner-scoped: an implementation must confirm, in the same
// atomic operation as the write, that playlistId belongs to userId, and return
// ErrPlaylistNotOwned (mutating nothing) when it does not. This is defense in
// depth beneath the service-layer ownership check, not a replacement for it.
type PlaylistMembershipRepository interface {
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId, position int) error
	AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
	RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error
	ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
