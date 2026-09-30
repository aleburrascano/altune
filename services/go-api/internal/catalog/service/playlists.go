package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"log/slog"
	"time"
)

const MaxPlaylistsPerUser = 1_000

var maxPlaylistsPerUser = MaxPlaylistsPerUser

var ErrTooManyPlaylists = &domain.CodedError{
	Msg:    "playlist limit reached",
	Status: 400,
	Code:   "catalog.too_many_playlists",
}

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
	if err := s.requirePlaylistSpace(ctx, userId); err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, wrapRepoError(ctx, "create playlist", err)
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.TypePlaylistCreated, map[string]any{
		"playlist_id": playlist.ID.String(),
		"name":        name,
	})
	return playlist, nil
}

func (s *PlaylistLifecycleService) requirePlaylistSpace(ctx context.Context, userId shared.UserId) error {
	held, err := s.playlistRepo.CountForUser(ctx, userId, maxPlaylistsPerUser)
	if err != nil {
		return wrapRepoError(ctx, "count playlists", err)
	}
	if held >= maxPlaylistsPerUser {
		slog.WarnContext(ctx, "catalog.too_many_playlists", "user_id", userId.String(), "cap", maxPlaylistsPerUser)
		return ErrTooManyPlaylists
	}
	return nil
}

func (s *PlaylistLifecycleService) List(ctx context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	limit, err := normalizePage(limit, offset)
	if err != nil {
		return nil, err
	}
	result, err := s.playlistRepo.ListForUser(ctx, userId, limit, offset)
	if err != nil {
		return nil, wrapRepoError(ctx, "list playlists", err)
	}
	return result, nil
}

func (s *PlaylistLifecycleService) Get(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, []*domain.Track, error) {
	playlist, tracks, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, nil, wrapRepoError(ctx, "get playlist with tracks", err)
	}
	if playlist == nil {
		return nil, nil, ErrPlaylistNotFound
	}
	return playlist, tracks, nil
}

func (s *PlaylistLifecycleService) Delete(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) error {
	deleted, err := s.playlistRepo.Delete(ctx, playlistId, userId)
	if err != nil {
		return wrapRepoError(ctx, "delete playlist", err)
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
		return nil, domain.PlaylistSummary{}, wrapRepoError(ctx, "rename playlist", err)
	}
	if playlist == nil {
		return nil, domain.PlaylistSummary{}, ErrPlaylistNotFound
	}
	if err := playlist.Rename(name, s.now()); err != nil {
		return nil, domain.PlaylistSummary{}, err
	}
	if err := s.playlistRepo.Update(ctx, playlist); err != nil {
		return nil, domain.PlaylistSummary{}, renameWriteError(ctx, err)
	}
	s.events.Publish(ctx, userId, events.TypePlaylistRenamed, map[string]any{
		"playlist_id": playlistId.String(),
		"name":        playlist.Name,
	})
	return playlist, summary, nil
}

func renameWriteError(ctx context.Context, err error) error {
	if errors.Is(err, ports.ErrPlaylistNotOwned) {
		return ErrPlaylistNotFound
	}
	return wrapRepoError(ctx, "rename playlist", err)
}
