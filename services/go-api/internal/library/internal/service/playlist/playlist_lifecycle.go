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
	source       ids.Source
}

var (
	ErrCollaboratorRequired = errors.New("playlist: a required collaborator is missing")
	ErrClockRequired        = errors.New("playlist: the service requires a clock")
	ErrIDSourceRequired     = errors.New("playlist: the service requires an id source")
)

func NewPlaylistLifecycleService(playlistRepo ports.PlaylistLifecycleRepository, publisher events.Publisher, now func() time.Time, source ids.Source) (*PlaylistLifecycleService, error) {
	switch {
	case playlistRepo == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: the playlist repository: %w", ErrCollaboratorRequired)
	case publisher == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: the events publisher: %w", ErrCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: %w", ErrClockRequired)
	case source == nil:
		return nil, fmt.Errorf("NewPlaylistLifecycleService: %w", ErrIDSourceRequired)
	}
	return &PlaylistLifecycleService{playlistRepo: playlistRepo, events: publisher, now: now, source: source}, nil
}

func MustNewPlaylistLifecycleService(playlistRepo ports.PlaylistLifecycleRepository, publisher events.Publisher, now func() time.Time, source ids.Source) *PlaylistLifecycleService {
	s, err := NewPlaylistLifecycleService(playlistRepo, publisher, now, source)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *PlaylistLifecycleService) Create(ctx context.Context, userId ids.UserId, name string, idempotencyKey *string) (*domain.Playlist, bool, error) {
	if idempotencyKey != nil {
		if err := domain.ValidateIdempotencyKey(*idempotencyKey); err != nil {
			return nil, false, fmt.Errorf("validate idempotency key: %w", err)
		}
		existing, found, err := s.playlistRepo.GetByIdempotencyKey(ctx, userId, *idempotencyKey)
		if err != nil {
			return nil, false, domain.WrapRepoError(ctx, "find playlist by idempotency key", err)
		}
		if found {
			return existing, false, nil
		}
	}
	playlist, err := domain.NewPlaylist(userId, name, s.now(), s.source)
	if err != nil {
		return nil, false, fmt.Errorf("new playlist: %w", err)
	}
	if err := s.requirePlaylistSpace(ctx, userId); err != nil {
		return nil, false, err
	}
	stored, created, err := s.store(ctx, playlist, idempotencyKey)
	if err != nil {
		return nil, false, domain.WrapRepoError(ctx, "create playlist", err)
	}
	if !created {
		return stored, false, nil
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", stored.ID.String())
	s.events.Publish(ctx, userId, events.PlaylistCreated{
		PlaylistID: stored.ID.String(),
		Name:       stored.Name,
	})
	return stored, true, nil
}

func (s *PlaylistLifecycleService) store(ctx context.Context, playlist *domain.Playlist, idempotencyKey *string) (*domain.Playlist, bool, error) {
	if idempotencyKey != nil {
		return s.playlistRepo.CreateKeyed(ctx, playlist, *idempotencyKey)
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, false, err
	}
	return playlist, true, nil
}

func (s *PlaylistLifecycleService) requirePlaylistSpace(ctx context.Context, userId ids.UserId) error {
	held, err := s.playlistRepo.CountForUser(ctx, userId, MaxPlaylistsPerUser)
	if err != nil {
		return domain.WrapRepoError(ctx, "count playlists", err)
	}
	if held >= MaxPlaylistsPerUser {
		slog.WarnContext(ctx, "catalog.too_many_playlists", "cap", MaxPlaylistsPerUser)
		return ErrTooManyPlaylists
	}
	return nil
}

func (s *PlaylistLifecycleService) List(ctx context.Context, userId ids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	limit, err := domain.NormalizePage(limit, offset)
	if err != nil {
		return nil, fmt.Errorf("normalize page: %w", err)
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
		"playlist_id", playlistId.String())
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
		return nil, domain.PlaylistSummary{}, fmt.Errorf("rename: %w", err)
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
