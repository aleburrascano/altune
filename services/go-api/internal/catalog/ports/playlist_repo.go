package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

// ErrPlaylistNotOwned is returned by a playlist write when the playlist does
// not exist or is not owned by the supplied user. The write is refused
// atomically at the data layer, so no row is touched.
var ErrPlaylistNotOwned = errors.New("playlist not found for user")

// ErrTrackMissing is returned by a membership write when a track it was asked
// to add no longer exists: it was deleted after the caller looked it up. The
// insert is refused atomically, so the playlist is unchanged.
var ErrTrackMissing = errors.New("track no longer exists")

// ErrPlaylistChangedDuringReorder is returned by ReorderTracks when the plan it
// was handed no longer matches the playlist's membership, because an add or a
// remove committed after the caller read the order. It is a validation error:
// the plan is a stale description of the playlist, and the caller's answer is
// to re-read the order and reorder again.
var ErrPlaylistChangedDuringReorder = domain.NewValidationError("playlist changed during reorder")

// PlaylistLifecycleRepository persists playlists and their metadata.
type PlaylistLifecycleRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	// ListForUser returns one page of the user's playlists, newest first. The
	// caller supplies an already-clamped limit; the page order must be total, so
	// that a later offset cannot repeat or skip a row its neighbour page held.
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error)
	// CountForUser counts the user's playlists, stopping at atMost so the
	// answer costs the same however many they own. A result of atMost means
	// "at least atMost", which is all the per-user cap check acts on.
	CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
	// Update rewrites the playlist's own row, and returns ErrPlaylistNotOwned
	// when no row of that owner matched, so a write racing a delete cannot
	// report success having changed nothing.
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
	// domain.ErrTrackAlreadyInPlaylist, writing nothing, when it is already a member,
	// and ErrTrackMissing when the track no longer exists.
	AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	// AddTracks appends, in the given order, each of trackIds that is not
	// already a member, and returns the ids it actually inserted. It returns
	// ErrTrackMissing, inserting none of them, when one no longer exists.
	AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (added []domain.TrackId, err error)
	// RemoveTrack removes trackId, closing the gap it leaves, and reports
	// whether it was a member.
	RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (removed bool, err error)
	// RemoveTracks removes every member among trackIds, closing the gaps they
	// leave, and returns the ids it actually removed.
	RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (removed []domain.TrackId, err error)
	// ReorderTracks sets each listed track's position. The caller builds tracks
	// from an unlocked read, so an implementation must re-check the membership
	// under its own lock and return ErrPlaylistChangedDuringReorder, writing
	// nothing, when the plan no longer covers exactly the playlist's tracks.
	ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}
