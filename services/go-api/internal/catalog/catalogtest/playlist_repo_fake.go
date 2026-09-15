package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
)

type PlaylistRepo struct {
	Playlists      map[string]*domain.Playlist
	PlaylistTracks map[string][]*domain.Track

	ErrOnCreate        error
	ErrOnGetByID       error
	ErrOnGetWithTracks error
	ErrOnList          error
	ErrOnDelete        error
	ErrOnUpdate        error
	ErrOnAddTrack      error
	ErrOnAddTracks     error
	ErrOnRemoveTrack   error
	ErrOnRemoveTracks  error
	ErrOnReorder       error

	Added   []domain.PlaylistTrack
	Removed []domain.TrackId
}

var (
	_ ports.PlaylistLifecycleRepository  = (*PlaylistRepo)(nil)
	_ ports.PlaylistMembershipRepository = (*PlaylistRepo)(nil)
)

func NewPlaylistRepo() *PlaylistRepo {
	return &PlaylistRepo{
		Playlists:      make(map[string]*domain.Playlist),
		PlaylistTracks: make(map[string][]*domain.Track),
	}
}

func (r *PlaylistRepo) Create(_ context.Context, playlist *domain.Playlist) error {
	if r.ErrOnCreate != nil {
		return r.ErrOnCreate
	}
	r.Playlists[playlist.ID.String()] = playlist
	return nil
}

func (r *PlaylistRepo) ListForUser(_ context.Context, userId shared.UserId) ([]domain.PlaylistWithSummary, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	var result []domain.PlaylistWithSummary
	for _, p := range r.Playlists {
		if p.UserId == userId {
			tracks := r.PlaylistTracks[p.ID.String()]
			result = append(result, domain.PlaylistWithSummary{
				Playlist: p,
				Summary: domain.PlaylistSummary{
					TrackCount:         len(tracks),
					PreviewArtworkURLs: domain.PreviewArtworkURLs(tracks),
				},
			})
		}
	}
	return result, nil
}

func (r *PlaylistRepo) GetByID(_ context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	if r.ErrOnGetByID != nil {
		return nil, domain.PlaylistSummary{}, r.ErrOnGetByID
	}
	p, ok := r.Playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, domain.PlaylistSummary{}, nil
	}
	return p, domain.PlaylistSummary{}, nil
}

func (r *PlaylistRepo) GetWithTracks(_ context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error) {
	if r.ErrOnGetWithTracks != nil {
		return nil, nil, r.ErrOnGetWithTracks
	}
	p, ok := r.Playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil, nil
	}
	return p, r.PlaylistTracks[id.String()], nil
}

func (r *PlaylistRepo) Delete(_ context.Context, id domain.PlaylistId, userId shared.UserId) (bool, error) {
	if r.ErrOnDelete != nil {
		return false, r.ErrOnDelete
	}
	key := id.String()
	p, ok := r.Playlists[key]
	if !ok || p.UserId != userId {
		return false, nil
	}
	delete(r.Playlists, key)
	return true, nil
}

func (r *PlaylistRepo) Update(_ context.Context, playlist *domain.Playlist) error {
	if r.ErrOnUpdate != nil {
		return r.ErrOnUpdate
	}
	r.Playlists[playlist.ID.String()] = playlist
	return nil
}

// ownedBy mirrors the owner-scoped SQL of the real adapter: a membership write
// to a playlist that is missing or owned by someone else is refused with
// ports.ErrPlaylistNotOwned and mutates nothing.
func (r *PlaylistRepo) ownedBy(playlistId domain.PlaylistId, userId shared.UserId) error {
	p, ok := r.Playlists[playlistId.String()]
	if !ok || p.UserId != userId {
		return ports.ErrPlaylistNotOwned
	}
	return nil
}

func (r *PlaylistRepo) AddTrack(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, _ domain.TrackId, _ int) error {
	if r.ErrOnAddTrack != nil {
		return r.ErrOnAddTrack
	}
	return r.ownedBy(playlistId, userId)
}

func (r *PlaylistRepo) AddTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if r.ErrOnAddTracks != nil {
		return r.ErrOnAddTracks
	}
	if err := r.ownedBy(playlistId, userId); err != nil {
		return err
	}
	r.Added = append(r.Added, tracks...)
	return nil
}

func (r *PlaylistRepo) RemoveTrack(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, _ domain.TrackId) error {
	if r.ErrOnRemoveTrack != nil {
		return r.ErrOnRemoveTrack
	}
	return r.ownedBy(playlistId, userId)
}

func (r *PlaylistRepo) RemoveTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	if r.ErrOnRemoveTracks != nil {
		return r.ErrOnRemoveTracks
	}
	if err := r.ownedBy(playlistId, userId); err != nil {
		return err
	}
	r.Removed = append(r.Removed, trackIds...)
	return nil
}

func (r *PlaylistRepo) ReorderTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, _ []domain.PlaylistTrack) error {
	if r.ErrOnReorder != nil {
		return r.ErrOnReorder
	}
	return r.ownedBy(playlistId, userId)
}

func (r *PlaylistRepo) Seed(playlist *domain.Playlist) {
	r.Playlists[playlist.ID.String()] = playlist
}

func (r *PlaylistRepo) SeedWithTracks(playlist *domain.Playlist, tracks []*domain.Track) {
	r.Playlists[playlist.ID.String()] = playlist
	r.PlaylistTracks[playlist.ID.String()] = tracks
}
