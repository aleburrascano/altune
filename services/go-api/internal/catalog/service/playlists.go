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

type PlaylistService struct {
	playlistRepo ports.PlaylistRepository
	trackRepo    ports.TrackRepository
	events       events.Publisher
}

func NewPlaylistService(playlistRepo ports.PlaylistRepository, trackRepo ports.TrackRepository, opts ...func(*PlaylistService)) *PlaylistService {
	s := &PlaylistService{playlistRepo: playlistRepo, trackRepo: trackRepo}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithPlaylistEvents(pub events.Publisher) func(*PlaylistService) {
	return func(s *PlaylistService) { s.events = pub }
}

func (s *PlaylistService) Create(ctx context.Context, userId shared.UserId, name string) (*domain.Playlist, error) {
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, fmt.Errorf("create playlist: %w", err)
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	if s.events != nil {
		s.events.Publish(userId, "playlist_created", map[string]any{
			"playlist_id": playlist.ID.String(),
			"name":        name,
		})
	}
	return playlist, nil
}

func (s *PlaylistService) GetByID(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, error) {
	p, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return nil, fmt.Errorf("get playlist: %w", err)
	}
	return p, nil
}

func (s *PlaylistService) List(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error) {
	playlists, err := s.playlistRepo.ListForUser(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	return playlists, nil
}

func (s *PlaylistService) GetPreviewArtwork(ctx context.Context, playlistId domain.PlaylistId) ([]string, error) {
	urls, err := s.playlistRepo.GetPreviewArtwork(ctx, playlistId)
	if err != nil {
		return nil, fmt.Errorf("get preview artwork: %w", err)
	}
	if urls == nil {
		return []string{}, nil
	}
	return urls, nil
}

func (s *PlaylistService) Get(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, []*domain.Track, error) {
	playlist, tracks, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, nil, fmt.Errorf("get playlist with tracks: %w", err)
	}
	if playlist == nil {
		return nil, nil, ErrPlaylistNotFound
	}
	return playlist, tracks, nil
}

func (s *PlaylistService) Delete(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) error {
	deleted, err := s.playlistRepo.Delete(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	if !deleted {
		return ErrPlaylistNotFound
	}
	slog.InfoContext(ctx, "playlist deleted",
		"playlist_id", playlistId.String(), "user_id", userId.String())
	if s.events != nil {
		s.events.Publish(userId, "playlist_deleted", map[string]any{
			"playlist_id": playlistId.String(),
		})
	}
	return nil
}

func (s *PlaylistService) Rename(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, name string) error {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("rename playlist: %w", err)
	}
	if playlist == nil {
		return ErrPlaylistNotFound
	}
	if err := playlist.Rename(name); err != nil {
		return err
	}
	if err := s.playlistRepo.Update(ctx, playlist); err != nil {
		return fmt.Errorf("rename playlist: %w", err)
	}
	if s.events != nil {
		s.events.Publish(userId, "playlist_renamed", map[string]any{
			"playlist_id": playlistId.String(),
			"name":        playlist.Name,
		})
	}
	return nil
}

// AIDEV-NOTE: AddTrack reads (track existence + playlist) then writes without a
// surrounding transaction, leaving a narrow race: a concurrent track delete
// between the GetByID checks and repo.AddTrack can append a now-missing track.
// Outcome is a soft failure (the row's FK/next read reconciles; client retries),
// not corruption — accepted pre-launch. Harden with a tx or FK-on-insert when a
// spec needs stronger atomicity.
func (s *PlaylistService) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return false, fmt.Errorf("add track to playlist: %w", err)
	}
	if playlist == nil {
		return false, ErrPlaylistNotFound
	}

	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return false, fmt.Errorf("add track to playlist: %w", err)
	}
	if track == nil {
		return false, ErrTrackNotFound
	}

	if err := playlist.AddTrack(trackId); err != nil {
		return false, err
	}

	if err := s.playlistRepo.AddTrack(ctx, playlistId, trackId, len(playlist.Tracks)-1); err != nil {
		return false, fmt.Errorf("add track to playlist: %w", err)
	}

	slog.InfoContext(ctx, "track added to playlist",
		"playlist_id", playlistId.String(), "track_id", trackId.String())
	if s.events != nil {
		s.events.Publish(userId, "track_added_to_playlist", map[string]any{
			"playlist_id": playlistId.String(),
			"track_id":    trackId.String(),
		})
	}
	return true, nil
}

func (s *PlaylistService) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	// AIDEV-NOTE: removal goes THROUGH the aggregate (like Reorder), not straight
	// to the repo. Playlist.RemoveTrack is the single authority for the
	// contiguous-position invariant — it decides membership and renumbers; the
	// repo's atomic DELETE+renumber persists the same result. This keeps remove
	// and reorder consistent (both: GetWithTracks → aggregate op → persist).
	playlist, _, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return false, fmt.Errorf("remove track from playlist: %w", err)
	}
	if playlist == nil {
		return false, ErrPlaylistNotFound
	}

	if !playlist.RemoveTrack(trackId) {
		// Track was not in the playlist — nothing to persist.
		return false, nil
	}

	if err := s.playlistRepo.RemoveTrack(ctx, playlistId, trackId); err != nil {
		return false, fmt.Errorf("remove track from playlist: %w", err)
	}
	if s.events != nil {
		s.events.Publish(userId, "track_removed_from_playlist", map[string]any{
			"playlist_id": playlistId.String(),
			"track_id":    trackId.String(),
		})
	}
	return true, nil
}

func (s *PlaylistService) Reorder(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
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
	if s.events != nil {
		ids := make([]string, len(playlist.Tracks))
		for i, pt := range playlist.Tracks {
			ids[i] = pt.TrackId.String()
		}
		s.events.Publish(userId, "playlist_reordered", map[string]any{
			"playlist_id": playlistId.String(),
			"track_ids":   ids,
		})
	}
	return nil
}
