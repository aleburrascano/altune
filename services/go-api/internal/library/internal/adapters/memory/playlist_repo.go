package memory

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"slices"
	"sort"
)

type PlaylistRepo struct {
	Playlists map[string]*domain.Playlist
	Tracks    *TrackRepo

	ErrOnCreate        error
	ErrOnCount         error
	ErrOnGetByID       error
	ErrOnGetWithTracks error
	ErrOnExists        error
	ErrOnGetTrackOrder error
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
		Playlists: make(map[string]*domain.Playlist),
		Tracks:    NewTrackRepo(),
	}
}

func (r *PlaylistRepo) Create(_ context.Context, playlist *domain.Playlist) error {
	if r.ErrOnCreate != nil {
		return r.ErrOnCreate
	}
	r.Playlists[playlist.ID.String()] = playlist
	return nil
}

func (r *PlaylistRepo) ListForUser(_ context.Context, userId sharedids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	owned := r.summariesOwnedBy(userId)
	sort.Slice(owned, func(i, j int) bool { return newerFirst(owned[i].Playlist, owned[j].Playlist) })
	return playlistWindow(owned, limit, offset), nil
}

func (r *PlaylistRepo) CountForUser(_ context.Context, userId sharedids.UserId, atMost int) (int, error) {
	if r.ErrOnCount != nil {
		return 0, r.ErrOnCount
	}
	held := 0
	for _, p := range r.Playlists {
		if p.UserId != userId {
			continue
		}
		held++
		if held == atMost {
			break
		}
	}
	return held, nil
}

func (r *PlaylistRepo) summariesOwnedBy(userId sharedids.UserId) []domain.PlaylistWithSummary {
	var owned []domain.PlaylistWithSummary
	for _, p := range r.Playlists {
		if p.UserId != userId {
			continue
		}
		tracks := r.membersOf(p)
		owned = append(owned, domain.PlaylistWithSummary{
			Playlist: p,
			Summary: domain.PlaylistSummary{
				TrackCount:         len(tracks),
				PreviewArtworkURLs: domain.PreviewArtworkURLs(tracks),
			},
		})
	}
	return owned
}

func newerFirst(a, b *domain.Playlist) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

func playlistWindow(rows []domain.PlaylistWithSummary, limit, offset int) []domain.PlaylistWithSummary {
	if offset < 0 || offset >= len(rows) {
		return nil
	}
	return rows[offset:min(offset+limit, len(rows))]
}

func (r *PlaylistRepo) GetByID(_ context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	if r.ErrOnGetByID != nil {
		return nil, domain.PlaylistSummary{}, r.ErrOnGetByID
	}
	p, ok := r.Playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, domain.PlaylistSummary{}, nil
	}
	return p, domain.PlaylistSummary{TrackCount: len(r.membersOf(p))}, nil
}

func (r *PlaylistRepo) membersOf(p *domain.Playlist) []*domain.Track {
	var members []*domain.Track
	for _, pt := range p.Tracks {
		if t, ok := r.Tracks.Tracks[pt.TrackId.String()]; ok {
			members = append(members, t)
		}
	}
	return members
}

func (r *PlaylistRepo) GetWithTracks(_ context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, []*domain.Track, error) {
	if r.ErrOnGetWithTracks != nil {
		return nil, nil, r.ErrOnGetWithTracks
	}
	p, ok := r.Playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil, nil
	}
	return p, r.membersOf(p), nil
}

func (r *PlaylistRepo) Exists(_ context.Context, id domain.PlaylistId, userId sharedids.UserId) (bool, error) {
	if r.ErrOnExists != nil {
		return false, r.ErrOnExists
	}
	_, err := r.ownedBy(id, userId)
	return err == nil, nil
}

func (r *PlaylistRepo) GetTrackOrder(_ context.Context, id domain.PlaylistId, userId sharedids.UserId) ([]domain.TrackId, bool, error) {
	if r.ErrOnGetTrackOrder != nil {
		return nil, false, r.ErrOnGetTrackOrder
	}
	p, ok := r.Playlists[id.String()]
	if !ok || p == nil || p.UserId != userId {
		return nil, false, nil
	}
	tracks := p.Tracks
	ids := make([]domain.TrackId, len(tracks))
	for i, t := range tracks {
		ids[i] = t.TrackId
	}
	return ids, true, nil
}

func (r *PlaylistRepo) Delete(_ context.Context, id domain.PlaylistId, userId sharedids.UserId) (bool, error) {
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
	if _, err := r.ownedBy(playlist.ID, playlist.UserId); err != nil {
		return err
	}
	r.Playlists[playlist.ID.String()] = playlist
	return nil
}

func (r *PlaylistRepo) ownedBy(playlistId domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, error) {
	p, ok := r.Playlists[playlistId.String()]
	if !ok || p == nil || p.UserId != userId {
		return nil, ports.ErrPlaylistNotOwned
	}
	return p, nil
}

func (r *PlaylistRepo) AddTrack(_ context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	if r.ErrOnAddTrack != nil {
		return r.ErrOnAddTrack
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return err
	}
	return r.appendTrack(p, trackId)
}

func (r *PlaylistRepo) AddTracks(_ context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	if r.ErrOnAddTracks != nil {
		return nil, r.ErrOnAddTracks
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return nil, err
	}
	var added []domain.TrackId
	keptTracks, keptAdded := len(p.Tracks), len(r.Added)
	for _, id := range trackIds {
		err := r.appendTrack(p, id)
		if errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			continue
		}
		if errors.Is(err, domain.ErrPlaylistFull) {
			p.Tracks, r.Added = p.Tracks[:keptTracks], r.Added[:keptAdded]
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		added = append(added, id)
	}
	return added, nil
}

func (r *PlaylistRepo) appendTrack(p *domain.Playlist, trackId domain.TrackId) error {
	if err := p.AddTrack(trackId, p.UpdatedAt); err != nil {
		return err
	}
	r.Added = append(r.Added, p.Tracks[len(p.Tracks)-1])
	return nil
}

func (r *PlaylistRepo) RemoveTrack(_ context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	if r.ErrOnRemoveTrack != nil {
		return false, r.ErrOnRemoveTrack
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return false, err
	}
	return r.removeTrack(p, trackId), nil
}

func (r *PlaylistRepo) RemoveTracks(_ context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	if r.ErrOnRemoveTracks != nil {
		return nil, r.ErrOnRemoveTracks
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return nil, err
	}
	var removed []domain.TrackId
	for _, id := range trackIds {
		if r.removeTrack(p, id) {
			removed = append(removed, id)
		}
	}
	return removed, nil
}

func (r *PlaylistRepo) removeTrack(p *domain.Playlist, trackId domain.TrackId) bool {
	if !p.RemoveTrack(trackId, p.UpdatedAt) {
		return false
	}
	r.Removed = append(r.Removed, trackId)
	return true
}

func (r *PlaylistRepo) ReorderTracks(_ context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if r.ErrOnReorder != nil {
		return r.ErrOnReorder
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return err
	}
	if !coversExactly(tracks, p.Tracks) {
		return ports.ErrPlaylistChangedDuringReorder
	}
	p.Tracks = append([]domain.PlaylistTrack(nil), tracks...)
	return nil
}

func coversExactly(planned, members []domain.PlaylistTrack) bool {
	if len(planned) != len(members) {
		return false
	}
	for _, want := range planned {
		if !holdsTrack(members, want.TrackId) {
			return false
		}
	}
	return true
}

func holdsTrack(tracks []domain.PlaylistTrack, id domain.TrackId) bool {
	return slices.ContainsFunc(tracks, func(t domain.PlaylistTrack) bool { return t.TrackId == id })
}

func (r *PlaylistRepo) Seed(playlist *domain.Playlist) {
	r.Playlists[playlist.ID.String()] = playlist
}

func (r *PlaylistRepo) SeedWithTracks(t TB, playlist *domain.Playlist, tracks []*domain.Track) {
	t.Helper()
	r.Playlists[playlist.ID.String()] = playlist
	for _, track := range tracks {
		r.Tracks.Tracks[track.ID.String()] = track
		if holdsTrack(playlist.Tracks, track.ID) {
			continue
		}
		if err := playlist.AddTrack(track.ID, playlist.UpdatedAt); err != nil {
			t.Fatalf("seed track %s into playlist %s: %v", track.ID, playlist.ID, err)
		}
	}
}
