package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

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
type PlaylistMembershipRepository interface {
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	AddTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId, position int) error
	AddTracks(ctx context.Context, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
	RemoveTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId) error
	RemoveTracks(ctx context.Context, playlistId domain.PlaylistId, trackIds []domain.TrackId) error
	ReorderTracks(ctx context.Context, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
