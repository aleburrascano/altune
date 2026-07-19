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

type PlaylistLifecycleService struct {
	playlistRepo ports.PlaylistRepository
	events       events.Publisher
}

func NewPlaylistLifecycleService(playlistRepo ports.PlaylistRepository, opts ...func(*PlaylistLifecycleService)) *PlaylistLifecycleService {
	s := &PlaylistLifecycleService{playlistRepo: playlistRepo, events: events.NoopPublisher()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithPlaylistLifecycleEvents(pub events.Publisher) func(*PlaylistLifecycleService) {
	return func(s *PlaylistLifecycleService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func (s *PlaylistLifecycleService) Create(ctx context.Context, userId shared.UserId, name string) (*domain.Playlist, error) {
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, fmt.Errorf("create playlist: %w", err)
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	s.events.Publish(userId, "playlist_created", map[string]any{
		"playlist_id": playlist.ID.String(),
		"name":        name,
	})
	return playlist, nil
}

func (s *PlaylistLifecycleService) List(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error) {
	playlists, err := s.playlistRepo.ListForUser(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	return playlists, nil
}

func (s *PlaylistLifecycleService) Get(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, []*domain.Track, error) {
	playlist, tracks, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, nil, fmt.Errorf("get playlist with tracks: %w", err)
	}
	if playlist == nil {
		return nil, nil, ErrPlaylistNotFound
	}
	return playlist, tracks, nil
}

func (s *PlaylistLifecycleService) Delete(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) error {
	deleted, err := s.playlistRepo.Delete(ctx, playlistId, userId)
	if err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	if !deleted {
		return ErrPlaylistNotFound
	}
	slog.InfoContext(ctx, "playlist deleted",
		"playlist_id", playlistId.String(), "user_id", userId.String())
	s.events.Publish(userId, "playlist_deleted", map[string]any{
		"playlist_id": playlistId.String(),
	})
	return nil
}

// PreviewArtworkURLs selects up to domain.PreviewArtworkLimit distinct track
// artwork URLs in the order they appear, for a playlist's preview tile. Used by
// the Get handler path where tracks are already loaded in Go; the List path
// delegates this selection to the SQL projection instead.
func PreviewArtworkURLs(tracks []*domain.Track) []string {
	urls := []string{}
	seen := make(map[string]bool)
	for _, t := range tracks {
		if t.ArtworkURL != nil && !seen[*t.ArtworkURL] && len(urls) < domain.PreviewArtworkLimit {
			urls = append(urls, *t.ArtworkURL)
			seen[*t.ArtworkURL] = true
		}
	}
	return urls
}

func (s *PlaylistLifecycleService) Rename(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, name string) (*domain.Playlist, error) {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return nil, fmt.Errorf("rename playlist: %w", err)
	}
	if playlist == nil {
		return nil, ErrPlaylistNotFound
	}
	if err := playlist.Rename(name); err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Update(ctx, playlist); err != nil {
		return nil, fmt.Errorf("rename playlist: %w", err)
	}
	s.events.Publish(userId, "playlist_renamed", map[string]any{
		"playlist_id": playlistId.String(),
		"name":        playlist.Name,
	})
	return playlist, nil
}

