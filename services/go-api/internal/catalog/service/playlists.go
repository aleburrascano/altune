package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type PlaylistLifecycleService struct {
	playlistRepo ports.PlaylistLifecycleRepository
	events       events.Publisher
	now          func() time.Time
}

func NewPlaylistLifecycleService(playlistRepo ports.PlaylistLifecycleRepository, opts ...func(*PlaylistLifecycleService)) *PlaylistLifecycleService {
	s := &PlaylistLifecycleService{playlistRepo: playlistRepo, events: events.NoopPublisher(), now: time.Now}
	return applyOptions(s, opts)
}

func WithPlaylistLifecycleEvents(pub events.Publisher) func(*PlaylistLifecycleService) {
	return func(s *PlaylistLifecycleService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func (s *PlaylistLifecycleService) Create(ctx context.Context, userId shared.UserId, name string) (*domain.Playlist, error) {
	playlist, err := domain.NewPlaylist(userId, name, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, fmt.Errorf("create playlist: %w", err)
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.TypePlaylistCreated, map[string]any{
		"playlist_id": playlist.ID.String(),
		"name":        name,
	})
	return playlist, nil
}

// List serves one page of the user's playlists. A caller that names no limit is
// served the default page, never the whole collection: the payload has to stay
// bounded however many playlists the user owns.
func (s *PlaylistLifecycleService) List(ctx context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	limit, err := normalizePage(limit, offset)
	if err != nil {
		return nil, err
	}
	result, err := s.playlistRepo.ListForUser(ctx, userId, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	return result, nil
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
	s.events.Publish(ctx, userId, events.TypePlaylistDeleted, map[string]any{
		"playlist_id": playlistId.String(),
	})
	return nil
}

func (s *PlaylistLifecycleService) Rename(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, name string) (*domain.Playlist, domain.PlaylistSummary, error) {
	playlist, summary, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return nil, domain.PlaylistSummary{}, fmt.Errorf("rename playlist: %w", err)
	}
	if playlist == nil {
		return nil, domain.PlaylistSummary{}, ErrPlaylistNotFound
	}
	if err := playlist.Rename(name, s.now()); err != nil {
		return nil, domain.PlaylistSummary{}, err
	}
	if err := s.playlistRepo.Update(ctx, playlist); err != nil {
		return nil, domain.PlaylistSummary{}, fmt.Errorf("rename playlist: %w", err)
	}
	s.events.Publish(ctx, userId, events.TypePlaylistRenamed, map[string]any{
		"playlist_id": playlistId.String(),
		"name":        playlist.Name,
	})
	return playlist, summary, nil
}
