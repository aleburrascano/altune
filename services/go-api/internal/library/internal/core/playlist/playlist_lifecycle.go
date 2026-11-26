package playlist

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const MaxPlaylistsPerUser = 1_000

var ErrTooManyPlaylists = apierror.NewError(apierror.CodeCatalogTooManyPlaylists, "playlist limit reached")

type PlaylistLifecycleService struct {
	playlistRepo ports.PlaylistLifecycleRepository
	events       events.Publisher
	now          func() time.Time
}

var (
	ErrCollaboratorRequired = errors.New("playlist: a required collaborator is missing")
	ErrClockRequired        = errors.New("playlist: the service requires a clock")
)

func NewPlaylistLifecycleService(playlistRepo ports.PlaylistLifecycleRepository, publisher events.Publisher, now func() time.Time) (*PlaylistLifecycleService, error) {
	switch {
	case playlistRepo == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: the playlist repository: %w", ErrCollaboratorRequired)
	case publisher == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: the events publisher: %w", ErrCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: %w", ErrClockRequired)
	}
	return &PlaylistLifecycleService{playlistRepo: playlistRepo, events: publisher, now: now}, nil
}

func MustNewPlaylistLifecycleService(playlistRepo ports.PlaylistLifecycleRepository, publisher events.Publisher, now func() time.Time) *PlaylistLifecycleService {
	s, err := NewPlaylistLifecycleService(playlistRepo, publisher, now)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *PlaylistLifecycleService) Create(ctx context.Context, userId ids.UserId, name string) (*domain.Playlist, error) {
	playlist, err := domain.NewPlaylist(userId, name, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.requirePlaylistSpace(ctx, userId); err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, domain.WrapRepoError(ctx, "create playlist", err)
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.PlaylistCreated{
		PlaylistID: playlist.ID.String(),
		Name:       name,
	})
	return playlist, nil
}

func (s *PlaylistLifecycleService) requirePlaylistSpace(ctx context.Context, userId ids.UserId) error {
	held, err := s.playlistRepo.CountForUser(ctx, userId, MaxPlaylistsPerUser)
	if err != nil {
		return domain.WrapRepoError(ctx, "count playlists", err)
	}
	if held >= MaxPlaylistsPerUser {
		slog.WarnContext(ctx, "catalog.too_many_playlists", "user_id", userId.String(), "cap", MaxPlaylistsPerUser)
		return ErrTooManyPlaylists
	}
	return nil
}

func (s *PlaylistLifecycleService) List(ctx context.Context, userId ids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	limit, err := domain.NormalizePage(limit, offset)
	if err != nil {
		return nil, err
	}
	result, err := s.playlistRepo.ListForUser(ctx, userId, limit, offset)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "list playlists", err)
	}
	return result, nil
}

func (s *PlaylistLifecycleService) Get(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId) (*domain.Playlist, []*domain.Track, error) {
	playlist, tracks, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, nil, domain.WrapRepoError(ctx, "get playlist with tracks", err)
	}
	if playlist == nil {
		return nil, nil, domain.ErrPlaylistNotFound
	}
	return playlist, tracks, nil
}

func (s *PlaylistLifecycleService) Delete(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId) error {
	deleted, err := s.playlistRepo.Delete(ctx, playlistId, userId)
	if err != nil {
		return domain.WrapRepoError(ctx, "delete playlist", err)
	}
	if !deleted {
		return domain.ErrPlaylistNotFound
	}
	slog.InfoContext(ctx, "playlist deleted",
		"playlist_id", playlistId.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.PlaylistDeleted{PlaylistID: playlistId.String()})
	return nil
}

func (s *PlaylistLifecycleService) Rename(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, name string) (*domain.Playlist, domain.PlaylistSummary, error) {
	playlist, summary, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return nil, domain.PlaylistSummary{}, domain.WrapRepoError(ctx, "rename playlist", err)
	}
	if playlist == nil {
		return nil, domain.PlaylistSummary{}, domain.ErrPlaylistNotFound
	}
	if err := playlist.Rename(name, s.now()); err != nil {
		return nil, domain.PlaylistSummary{}, err
	}
	if err := s.playlistRepo.Update(ctx, playlist); err != nil {
		return nil, domain.PlaylistSummary{}, renameWriteError(ctx, err)
	}
	s.events.Publish(ctx, userId, events.PlaylistRenamed{
		PlaylistID: playlistId.String(),
		Name:       playlist.Name,
	})
	return playlist, summary, nil
}

func renameWriteError(ctx context.Context, err error) error {
	if errors.Is(err, ports.ErrPlaylistNotOwned) {
		return domain.ErrPlaylistNotFound
	}
	return domain.WrapRepoError(ctx, "rename playlist", err)
}
