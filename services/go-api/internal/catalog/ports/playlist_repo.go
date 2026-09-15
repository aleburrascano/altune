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
//
// Membership checks and position arithmetic happen inside the write, against
// targeted rows, so a single-track change costs the same whatever the playlist
// size: no method loads the whole track list except GetTrackOrder, which only
// Reorder needs.
type PlaylistMembershipRepository interface {
	// Exists reports whether playlistId exists and is owned by userId.
	Exists(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) (bool, error)
	// GetTrackOrder returns the playlist's track ids in position order, or
	// found=false when the playlist is missing or not owned by userId.
	GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) (trackIds []domain.TrackId, found bool, err error)
	// AddTrack appends trackId at the end of the playlist. It returns
	// domain.ErrTrackAlreadyInPlaylist, writing nothing, when it is already a member.
	AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	// AddTracks appends, in the given order, each of trackIds that is not
	// already a member, and returns the ids it actually inserted.
	AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (added []domain.TrackId, err error)
	// RemoveTrack removes trackId, closing the gap it leaves, and reports
	// whether it was a member.
	RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (removed bool, err error)
	// RemoveTracks removes every member among trackIds, closing the gaps they
	// leave, and returns the ids it actually removed.
	RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (removed []domain.TrackId, err error)
	// ReorderTracks sets each listed track's position.
	ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
