package ports

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type PlaylistLifecycleRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	CreateKeyed(ctx context.Context, playlist *domain.Playlist, idempotencyKey string) (stored *domain.Playlist, created bool, err error)
	GetByIdempotencyKey(ctx context.Context, userId ids.UserId, idempotencyKey string) (*domain.Playlist, bool, error)
	ListForUser(ctx context.Context, userId ids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error)
	CountForUser(ctx context.Context, userId ids.UserId, atMost int) (int, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
}

type PlaylistMembershipRepository interface {
	Exists(ctx context.Context, playlistId domain.PlaylistId, userId ids.UserId) (bool, error)
	GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId ids.UserId) (trackIds []domain.TrackId, found bool, err error)
	AddTrack(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	AddTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (added []domain.TrackId, err error)
	RemoveTrack(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (removed bool, err error)
	RemoveTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (removed []domain.TrackId, err error)
	ReorderTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
