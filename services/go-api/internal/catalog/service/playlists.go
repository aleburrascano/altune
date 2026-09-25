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

// MaxPlaylistsPerUser is the most playlists one account may create. Nothing
// else bounds them: names need not be distinct and the list is paged, so
// without a cap an authenticated caller grows the playlists table without
// limit (#2200). It sits far above any hand-curated collection.
const MaxPlaylistsPerUser = 1_000

// maxPlaylistsPerUser is the cap the check actually reads. It is a var so a
// test can cross it with a handful of rows instead of a thousand.
var maxPlaylistsPerUser = MaxPlaylistsPerUser

// ErrTooManyPlaylists refuses a create once the account already holds
// MaxPlaylistsPerUser playlists. The ones it has stay readable and editable;
// only further creates are refused.
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

// requirePlaylistSpace refuses the create once the account holds
// maxPlaylistsPerUser playlists. The count is read before the insert, so
// creates racing the last slot can all pass it: the cap bounds growth, it is
// not an exact quota, and the per-user write throttle in front of the route
// bounds the overshoot to the requests one caller has in flight.
func (s *PlaylistLifecycleService) requirePlaylistSpace(ctx context.Context, userId shared.UserId) error {
	held, err := s.playlistRepo.CountForUser(ctx, userId, maxPlaylistsPerUser)
	if err != nil {
		return fmt.Errorf("count playlists: %w", err)
	}
	if held >= maxPlaylistsPerUser {
		// The refusal is a coded 400, which the HTTP layer does not log, and an
		// account that has stopped being able to create is worth seeing without
		// a client report.
		slog.WarnContext(ctx, "catalog.too_many_playlists", "user_id", userId.String(), "cap", maxPlaylistsPerUser)
		return ErrTooManyPlaylists
	}
	return nil
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
		return nil, domain.PlaylistSummary{}, renameWriteError(err)
	}
	s.events.Publish(ctx, userId, events.TypePlaylistRenamed, map[string]any{
		"playlist_id": playlistId.String(),
		"name":        playlist.Name,
	})
	return playlist, summary, nil
}

// renameWriteError reports a rename the data layer refused because the caller
// no longer owns the playlist — deleted between the read and the write — as
// ErrPlaylistNotFound, so a rename that changed nothing cannot answer success
// and publish a rename of a playlist that is gone.
func renameWriteError(err error) error {
	if errors.Is(err, ports.ErrPlaylistNotOwned) {
		return ErrPlaylistNotFound
	}
	return fmt.Errorf("rename playlist: %w", err)
}
