package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

const MaxPlaylistBatchSize = 500

type PlaylistMembershipService struct {
	playlistRepo ports.PlaylistRepository
	trackRepo    ports.TrackRepository
	events       events.Publisher
}

func NewPlaylistMembershipService(playlistRepo ports.PlaylistRepository, trackRepo ports.TrackRepository, opts ...func(*PlaylistMembershipService)) *PlaylistMembershipService {
	s := &PlaylistMembershipService{playlistRepo: playlistRepo, trackRepo: trackRepo, events: events.NoopPublisher()}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

	if err := playlist.AddTrack(trackId); err != nil {
		return err
	}

	if err := s.playlistRepo.AddTrack(ctx, playlistId, trackId, len(playlist.Tracks)-1); err != nil {
		return fmt.Errorf("add track to playlist: %w", err)
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
		return 0, &domain.ValidationError{Message: "too many tracks in one request"}
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
		if err := playlist.AddTrack(id); err != nil && !errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			return 0, err
		}
	}

	added := playlist.Tracks[before:]
	if len(added) == 0 {
		return 0, nil
	}

	if err := s.playlistRepo.AddTracks(ctx, playlistId, added); err != nil {
		return 0, fmt.Errorf("add tracks to playlist: %w", err)
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

	if !playlist.RemoveTrack(trackId) {
		return nil
	}

	if err := s.playlistRepo.RemoveTrack(ctx, playlistId, trackId); err != nil {
		return fmt.Errorf("remove track from playlist: %w", err)
	}
	s.events.Publish(userId, "track_removed_from_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_id":    trackId.String(),
	})
	return nil
}

func (s *PlaylistMembershipService) RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (int, error) {
	if len(trackIds) > MaxPlaylistBatchSize {
		return 0, &domain.ValidationError{Message: "too many tracks in one request"}
	}

	playlist, err := s.loadPlaylist(ctx, playlistId, userId, "remove tracks from playlist")
	if err != nil {
		return 0, err
	}

	var removed []domain.TrackId
	for _, id := range trackIds {
		if playlist.RemoveTrack(id) {
			removed = append(removed, id)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}

	if err := s.playlistRepo.RemoveTracks(ctx, playlistId, removed); err != nil {
		return 0, fmt.Errorf("remove tracks from playlist: %w", err)
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

	if err := playlist.Reorder(trackIds); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}

	if err := s.playlistRepo.ReorderTracks(ctx, playlistId, playlist.Tracks); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}
	s.events.Publish(userId, "playlist_reordered", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   trackIdStrings(trackIds),
	})
	return nil
}
