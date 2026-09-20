package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const MaxPlaylistBatchSize = 500

// PlaylistMembershipService adds, removes and reorders playlist tracks. Add and
// remove never load the playlist's track list: membership and position are
// decided by targeted, owner-scoped repository writes, so their cost does not
// grow with the playlist. Only Reorder, which must validate the full order,
// reads the (id-only) track order.
type PlaylistMembershipService struct {
	playlistRepo ports.PlaylistMembershipRepository
	trackRepo    ports.TrackLookup
	events       events.Publisher
	now          func() time.Time
}

func NewPlaylistMembershipService(playlistRepo ports.PlaylistMembershipRepository, trackRepo ports.TrackLookup, opts ...func(*PlaylistMembershipService)) *PlaylistMembershipService {
	s := &PlaylistMembershipService{playlistRepo: playlistRepo, trackRepo: trackRepo, events: events.NoopPublisher(), now: time.Now}
	return applyOptions(s, opts)
}

func WithPlaylistMembershipEvents(pub events.Publisher) func(*PlaylistMembershipService) {
	return func(s *PlaylistMembershipService) {
		if pub != nil {
			s.events = pub
		}
	}
}

// requirePlaylist confirms the playlist exists and is owned by userId, wrapping
// any repository error with op and translating a missing playlist into
// ErrPlaylistNotFound. It reads only the playlist row.
func (s *PlaylistMembershipService) requirePlaylist(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId, op string) error {
	exists, err := s.playlistRepo.Exists(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if !exists {
		return ErrPlaylistNotFound
	}
	return nil
}

// membershipWriteError wraps a membership-write repository error with op. A
// write the data layer refused because the playlist is not owned by the caller
// (missing, or deleted after requirePlaylist) surfaces as ErrPlaylistNotFound,
// the same answer requirePlaylist gives, so the owner-scoped SQL never leaks as
// a 500; a write refused because the track itself is gone (deleted after the
// lookup) surfaces as ErrTrackNotFound, the same answer the lookup gives. A
// domain error the write reports (ErrTrackAlreadyInPlaylist, ErrPlaylistFull)
// passes through unwrapped, so its message reaches the client as the whole
// answer, and a validation error it reports keeps its 400 through the wrap.
func membershipWriteError(op string, err error) error {
	if errors.Is(err, ports.ErrPlaylistNotOwned) {
		return ErrPlaylistNotFound
	}
	if errors.Is(err, ports.ErrTrackMissing) {
		return ErrTrackNotFound
	}
	if errors.Is(err, domain.ErrTrackAlreadyInPlaylist) || errors.Is(err, domain.ErrPlaylistFull) {
		return err
	}
	return fmt.Errorf("%s: %w", op, err)
}

// trackIdStrings renders a slice of track ids as their string representations.
func trackIdStrings(ids []domain.TrackId) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func (s *PlaylistMembershipService) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	if err := s.requirePlaylist(ctx, playlistId, userId, "add track to playlist"); err != nil {
		return err
	}

	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("add track to playlist: %w", err)
	}
	if track == nil {
		return ErrTrackNotFound
	}

	if err := s.playlistRepo.AddTrack(ctx, userId, playlistId, trackId); err != nil {
		return membershipWriteError("add track to playlist", err)
	}

	slog.InfoContext(ctx, "track added to playlist",
		"playlist_id", playlistId.String(), "track_id", trackId.String())
	s.events.Publish(ctx, userId, events.TypeTrackAddedToPlaylist, map[string]any{
		"playlist_id": playlistId.String(),
		"track_id":    trackId.String(),
	})
	return nil
}

func (s *PlaylistMembershipService) AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (int, error) {
	if len(trackIds) > MaxPlaylistBatchSize {
		return 0, domain.NewValidationError("too many tracks in one request")
	}

	if err := s.requirePlaylist(ctx, playlistId, userId, "add tracks to playlist"); err != nil {
		return 0, err
	}

	candidates, err := s.ownedDistinct(ctx, userId, trackIds)
	if err != nil {
		return 0, fmt.Errorf("add tracks to playlist: %w", err)
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	added, err := s.playlistRepo.AddTracks(ctx, userId, playlistId, candidates)
	if err != nil {
		return 0, membershipWriteError("add tracks to playlist", err)
	}
	if len(added) == 0 {
		return 0, nil
	}

	slog.InfoContext(ctx, "tracks added to playlist",
		"playlist_id", playlistId.String(), "added", len(added), "requested", len(trackIds))
	s.events.Publish(ctx, userId, events.TypeTracksAddedToPlaylist, map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(added),
	})
	return len(added), nil
}

// ownedDistinct returns the ids among trackIds that userId owns, in request
// order and without repeats.
func (s *PlaylistMembershipService) ownedDistinct(ctx context.Context, userId shared.UserId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	tracks, err := s.trackRepo.ListByIDs(ctx, userId, trackIds)
	if err != nil {
		return nil, err
	}
	owned := make(map[domain.TrackId]bool, len(tracks))
	for _, t := range tracks {
		owned[t.ID] = true
	}
	out := make([]domain.TrackId, 0, len(owned))
	for _, id := range trackIds {
		if owned[id] {
			out = append(out, id)
			delete(owned, id)
		}
	}
	return out, nil
}

func (s *PlaylistMembershipService) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	removed, err := s.playlistRepo.RemoveTrack(ctx, userId, playlistId, trackId)
	if err != nil {
		return membershipWriteError("remove track from playlist", err)
	}
	if !removed {
		return nil
	}
	slog.InfoContext(ctx, "track removed from playlist",
		"playlist_id", playlistId.String(), "track_id", trackId.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.TypeTrackRemovedFromPlaylist, map[string]any{
		"playlist_id": playlistId.String(),
		"track_id":    trackId.String(),
	})
	return nil
}

func (s *PlaylistMembershipService) RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (int, error) {
	if len(trackIds) > MaxPlaylistBatchSize {
		return 0, domain.NewValidationError("too many tracks in one request")
	}

	removed, err := s.playlistRepo.RemoveTracks(ctx, userId, playlistId, trackIds)
	if err != nil {
		return 0, membershipWriteError("remove tracks from playlist", err)
	}
	if len(removed) == 0 {
		return 0, nil
	}

	slog.InfoContext(ctx, "tracks removed from playlist",
		"playlist_id", playlistId.String(), "user_id", userId.String(),
		"track_ids", trackIdStrings(removed), "removed", len(removed), "requested", len(trackIds))

	s.events.Publish(ctx, userId, events.TypeTracksRemovedFromPlaylist, map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(removed),
	})
	return len(removed), nil
}

func (s *PlaylistMembershipService) Reorder(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	current, found, err := s.playlistRepo.GetTrackOrder(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}
	if !found {
		return ErrPlaylistNotFound
	}

	playlist := &domain.Playlist{ID: playlistId, UserId: userId, Tracks: make([]domain.PlaylistTrack, len(current))}
	for i, id := range current {
		playlist.Tracks[i] = domain.PlaylistTrack{TrackId: id, Position: i}
	}
	if err := playlist.Reorder(trackIds, s.now()); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}

	if err := s.playlistRepo.ReorderTracks(ctx, userId, playlistId, playlist.Tracks); err != nil {
		return membershipWriteError("reorder playlist", err)
	}
	s.events.Publish(ctx, userId, events.TypePlaylistReordered, map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(trackIds),
	})
	return nil
}
