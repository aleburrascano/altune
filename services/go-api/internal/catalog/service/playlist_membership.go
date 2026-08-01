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

type playlistTrackReader interface {
	trackByIDGetter
	trackBatchReader
}

type PlaylistMembershipService struct {
	playlistRepo ports.PlaylistRepository
	trackRepo    playlistTrackReader
	events       events.Publisher
}

func NewPlaylistMembershipService(playlistRepo ports.PlaylistRepository, trackRepo playlistTrackReader, opts ...func(*PlaylistMembershipService)) *PlaylistMembershipService {
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

func (s *PlaylistMembershipService) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("add track to playlist: %w", err)
	}
	if playlist == nil {
		return ErrPlaylistNotFound
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

	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return 0, fmt.Errorf("add tracks to playlist: %w", err)
	}
	if playlist == nil {
		return 0, ErrPlaylistNotFound
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

	addedIds := make([]string, len(added))
	for i, pt := range added {
		addedIds[i] = pt.TrackId.String()
	}
	slog.InfoContext(ctx, "tracks added to playlist",
		"playlist_id", playlistId.String(), "added", len(added), "requested", len(trackIds))
	s.events.Publish(userId, "tracks_added_to_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   addedIds,
	})
	return len(added), nil
}

func (s *PlaylistMembershipService) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("remove track from playlist: %w", err)
	}
	if playlist == nil {
		return ErrPlaylistNotFound
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

	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return 0, fmt.Errorf("remove tracks from playlist: %w", err)
	}
	if playlist == nil {
		return 0, ErrPlaylistNotFound
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

	removedIds := make([]string, len(removed))
	for i, id := range removed {
		removedIds[i] = id.String()
	}
	s.events.Publish(userId, "tracks_removed_from_playlist", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   removedIds,
	})
	return len(removed), nil
}

func (s *PlaylistMembershipService) Reorder(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}
	if playlist == nil {
		return ErrPlaylistNotFound
	}

	if err := playlist.Reorder(trackIds); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}

	if err := s.playlistRepo.ReorderTracks(ctx, playlistId, playlist.Tracks); err != nil {
		return fmt.Errorf("reorder playlist: %w", err)
	}
	ids := make([]string, len(playlist.Tracks))
	for i, pt := range playlist.Tracks {
		ids[i] = pt.TrackId.String()
	}
	s.events.Publish(userId, "playlist_reordered", map[string]any{
		"playlist_id": playlistId.String(),
		"track_ids":   ids,
	})
	return nil
}
