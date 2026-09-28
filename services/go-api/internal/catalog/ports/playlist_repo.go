package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrPlaylistNotOwned = errors.New("playlist not found for user")

var ErrTrackMissing = errors.New("track no longer exists")

var ErrPlaylistChangedDuringReorder = domain.NewValidationError("playlist changed during reorder")

type PlaylistLifecycleRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error)
	CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
}

type PlaylistMembershipRepository interface {
	Exists(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) (bool, error)
	GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) (trackIds []domain.TrackId, found bool, err error)
	AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (added []domain.TrackId, err error)
	RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (removed bool, err error)
	RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (removed []domain.TrackId, err error)
	ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
