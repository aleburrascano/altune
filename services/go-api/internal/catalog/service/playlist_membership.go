package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

type PlaylistMembershipService struct {
	playlistRepo ports.PlaylistRepository
	trackRepo    trackByIDGetter
	events       events.Publisher
}

func NewPlaylistMembershipService(playlistRepo ports.PlaylistRepository, trackRepo trackByIDGetter, opts ...func(*PlaylistMembershipService)) *PlaylistMembershipService {
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
