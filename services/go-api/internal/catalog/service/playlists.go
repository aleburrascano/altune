package service

import (
	"context"
	"errors"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

var ErrPlaylistNotFound = errors.New("playlist not found")

type PlaylistService struct {
	playlistRepo ports.PlaylistRepository
	trackRepo    ports.TrackRepository
}

func NewPlaylistService(playlistRepo ports.PlaylistRepository, trackRepo ports.TrackRepository) *PlaylistService {
	return &PlaylistService{playlistRepo: playlistRepo, trackRepo: trackRepo}
}

func (s *PlaylistService) Create(ctx context.Context, userId shared.UserId, name string) (*domain.Playlist, error) {
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		return nil, err
	}
	if err := s.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "playlist created",
		"playlist_id", playlist.ID.String(), "user_id", userId.String())
	return playlist, nil
}

func (s *PlaylistService) GetByID(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, error) {
	return s.playlistRepo.GetByID(ctx, playlistId, userId)
}

func (s *PlaylistService) List(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error) {
	return s.playlistRepo.ListForUser(ctx, userId)
}

func (s *PlaylistService) Get(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) (*domain.Playlist, []*domain.Track, error) {
	playlist, tracks, err := s.playlistRepo.GetWithTracks(ctx, playlistId, userId)
	if err != nil {
		return nil, nil, err
	}
	if playlist == nil {
		return nil, nil, ErrPlaylistNotFound
	}
	return playlist, tracks, nil
}

func (s *PlaylistService) Delete(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId) error {
	deleted, err := s.playlistRepo.Delete(ctx, playlistId, userId)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrPlaylistNotFound
	}
	slog.InfoContext(ctx, "playlist deleted",
		"playlist_id", playlistId.String(), "user_id", userId.String())
	return nil
}

func (s *PlaylistService) Rename(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, name string) error {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return err
	}
	if playlist == nil {
		return ErrPlaylistNotFound
	}
	if err := playlist.Rename(name); err != nil {
		return err
	}
	return s.playlistRepo.Update(ctx, playlist)
}

func (s *PlaylistService) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return false, err
	}
	if playlist == nil {
		return false, ErrPlaylistNotFound
	}

	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return false, err
	}
	if track == nil {
		return false, ErrTrackNotFound
	}

	if err := playlist.AddTrack(trackId); err != nil {
		return false, nil
	}

	if err := s.playlistRepo.AddTrack(ctx, playlistId, trackId, len(playlist.Tracks)-1); err != nil {
		return false, err
	}

	slog.InfoContext(ctx, "track added to playlist",
		"playlist_id", playlistId.String(), "track_id", trackId.String())
	return true, nil
}

func (s *PlaylistService) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return false, err
	}
	if playlist == nil {
		return false, ErrPlaylistNotFound
	}

	if err := s.playlistRepo.RemoveTrack(ctx, playlistId, trackId); err != nil {
		return false, err
	}
	return true, nil
}

func (s *PlaylistService) Reorder(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	playlist, err := s.playlistRepo.GetByID(ctx, playlistId, userId)
	if err != nil {
		return err
	}
	if playlist == nil {
		return ErrPlaylistNotFound
	}

	tracks := make([]domain.PlaylistTrack, len(trackIds))
	for i, id := range trackIds {
		tracks[i] = domain.PlaylistTrack{TrackId: id, Position: i}
	}
	return s.playlistRepo.ReorderTracks(ctx, playlistId, tracks)
}
