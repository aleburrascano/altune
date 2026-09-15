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

// loadPlaylist fetches a playlist with its tracks, wrapping any repository error
// with op and translating a missing playlist into ErrPlaylistNotFound.
func (s *PlaylistMembershipService) loadPlaylist(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId, op string) (*domain.Playlist, error) {
	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if playlist == nil {
		return nil, ErrPlaylistNotFound
	}
	return playlist, nil
}

// membershipWriteError wraps a membership-write repository error with op. A
// write the data layer refused because the playlist is not owned by the caller
// (for example, deleted between loadPlaylist and the write) surfaces as
// ErrPlaylistNotFound, the same answer loadPlaylist gives, so the owner-scoped
// SQL never leaks as a 500.
func membershipWriteError(op string, err error) error {
	if errors.Is(err, ports.ErrPlaylistNotOwned) {
		return ErrPlaylistNotFound
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
	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "add track to playlist")
	if err != nil {
		return err
	}

	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("add track to playlist: %w", err)
	}
	if track == nil {
		return ErrTrackNotFound
	}

	if err := playlist.AddTrack(trackId, s.now()); err != nil {
		return err
	}

	if err := s.playlistRepo.AddTrack(ctx, userId, playlistId, trackId, len(playlist.Tracks)-1); err != nil {
		return membershipWriteError("add track to playlist", err)
	}

	slog.InfoContext(ctx, "track added to playlist",
		"playlist_id", playlistId.String(), "track_id", trackId.String())
	s.events.Publish(userId, "track_added_to_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_id":    trackId.String(),
	})
	return nil
}

func (s *PlaylistMembershipService) AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (int, error) {
	if len(trackIds) > MaxPlaylistBatchSize {
		return 0, domain.NewValidationError("too many tracks in one request")
	}

	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "add tracks to playlist")
	if err != nil {
		return 0, err
	}

	tracks, err := s.trackRepo.ListByIDs(ctx, userId, trackIds)
	if err != nil {
		return 0, fmt.Errorf("add tracks to playlist: %w", err)
	}
	owned := make(map[domain.TrackId]bool, len(tracks))
	for _, t := range tracks {
		owned[t.ID] = true
	}

	before := len(playlist.Tracks)
	for _, id := range trackIds {
		if !owned[id] {
			continue
		}
		if err := playlist.AddTrack(id, s.now()); err != nil && !errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			return 0, err
		}
	}

	added := playlist.Tracks[before:]
	if len(added) == 0 {
		return 0, nil
	}

	if err := s.playlistRepo.AddTracks(ctx, userId, playlistId, added); err != nil {
		return 0, membershipWriteError("add tracks to playlist", err)
	}

	addedIds := make([]domain.TrackId, len(added))
	for i, pt := range added {
		addedIds[i] = pt.TrackId
	}
	slog.InfoContext(ctx, "tracks added to playlist",
		"playlist_id", playlistId.String(), "added", len(added), "requested", len(trackIds))
	s.events.Publish(userId, "tracks_added_to_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(addedIds),
	})
	return len(added), nil
}

func (s *PlaylistMembershipService) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "remove track from playlist")
	if err != nil {
		return err
	}

	if !playlist.RemoveTrack(trackId, s.now()) {
		return nil
	}

	if err := s.playlistRepo.RemoveTrack(ctx, userId, playlistId, trackId); err != nil {
		return membershipWriteError("remove track from playlist", err)
	}
	s.events.Publish(userId, "track_removed_from_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_id":    trackId.String(),
	})
	return nil
}

func (s *PlaylistMembershipService) RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (int, error) {
	if len(trackIds) > MaxPlaylistBatchSize {
		return 0, domain.NewValidationError("too many tracks in one request")
	}

	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "remove tracks from playlist")
	if err != nil {
		return 0, err
	}

	var removed []domain.TrackId
	for _, id := range trackIds {
		if playlist.RemoveTrack(id, s.now()) {
			removed = append(removed, id)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}

	if err := s.playlistRepo.RemoveTracks(ctx, userId, playlistId, removed); err != nil {
		return 0, membershipWriteError("remove tracks from playlist", err)
	}

	s.events.Publish(userId, "tracks_removed_from_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(removed),
	})
	return len(removed), nil
}

func (s *PlaylistMembershipService) Reorder(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "reorder playlist")
	if err != nil {
		return err
	}

	if err := playlist.Reorder(trackIds, s.now()); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}

	if err := s.playlistRepo.ReorderTracks(ctx, userId, playlistId, playlist.Tracks); err != nil {
		return membershipWriteError("reorder playlist", err)
	}
	s.events.Publish(userId, "playlist_reordered", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(trackIds),
	})
	return nil
}
