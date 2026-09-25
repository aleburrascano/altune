package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"maps"
	"sort"
)

// PlaylistRepo is an in-memory playlist repository. Membership lives on each
// seeded playlist's Tracks slice: the membership writes decide duplicates,
// positions and removals against it the way the real adapter does in SQL.
type PlaylistRepo struct {
	Playlists      map[string]*domain.Playlist
	PlaylistTracks map[string][]*domain.Track

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

func (r *PlaylistRepo) ListForUser(_ context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	owned := r.summariesOwnedBy(userId)
	sort.Slice(owned, func(i, j int) bool { return newerFirst(owned[i].Playlist, owned[j].Playlist) })
	return playlistWindow(owned, limit, offset), nil
}

// CountForUser mirrors the adapter's bounded count: it stops at atMost, so a
// caller cannot tell an account exactly at the cap from one far past it.
func (r *PlaylistRepo) CountForUser(_ context.Context, userId shared.UserId, atMost int) (int, error) {
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

func (r *PlaylistRepo) summariesOwnedBy(userId shared.UserId) []domain.PlaylistWithSummary {
	var owned []domain.PlaylistWithSummary
	for _, p := range r.Playlists {
		if p.UserId != userId {
			continue
		}
		tracks := r.PlaylistTracks[p.ID.String()]
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

// newerFirst is the adapter's "created_at DESC, id DESC". Map iteration has no
// order of its own, so a paged read of this fake would otherwise repeat and skip
// rows that the real one cannot.
func newerFirst(a, b *domain.Playlist) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

// playlistWindow pages rows the way LIMIT/OFFSET does. An offset past the end
// pages to nothing, as does a negative one — which the service refuses long
// before any repository sees it.
func playlistWindow(rows []domain.PlaylistWithSummary, limit, offset int) []domain.PlaylistWithSummary {
	if offset < 0 || offset >= len(rows) {
		return nil
	}
	return rows[offset:min(offset+limit, len(rows))]
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

func (r *PlaylistRepo) Exists(_ context.Context, id domain.PlaylistId, userId shared.UserId) (bool, error) {
	if r.ErrOnExists != nil {
		return false, r.ErrOnExists
	}
	_, err := r.ownedBy(id, userId)
	return err == nil, nil
}

func (r *PlaylistRepo) GetTrackOrder(_ context.Context, id domain.PlaylistId, userId shared.UserId) ([]domain.TrackId, bool, error) {
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
	if _, err := r.ownedBy(playlist.ID, playlist.UserId); err != nil {
		return err
	}
	r.Playlists[playlist.ID.String()] = playlist
	return nil
}

// ownedBy mirrors the owner-scoped SQL of the real adapter: a membership write
// to a playlist that is missing or owned by someone else is refused with
// ports.ErrPlaylistNotOwned and mutates nothing.
func (r *PlaylistRepo) ownedBy(playlistId domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error) {
	p, ok := r.Playlists[playlistId.String()]
	if !ok || p == nil || p.UserId != userId {
		return nil, ports.ErrPlaylistNotOwned
	}
	return p, nil
}

func (r *PlaylistRepo) AddTrack(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	if r.ErrOnAddTrack != nil {
		return r.ErrOnAddTrack
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return err
	}
	return r.appendTrack(p, trackId)
}

func (r *PlaylistRepo) AddTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	if r.ErrOnAddTracks != nil {
		return nil, r.ErrOnAddTracks
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return nil, err
	}
	var added []domain.TrackId
	for _, id := range trackIds {
		err := r.appendTrack(p, id)
		if errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		added = append(added, id)
	}
	return added, nil
}

// appendTrack adds trackId to p, recording the persisted row in Added.
func (r *PlaylistRepo) appendTrack(p *domain.Playlist, trackId domain.TrackId) error {
	if err := p.AddTrack(trackId, p.UpdatedAt); err != nil {
		return err
	}
	r.Added = append(r.Added, p.Tracks[len(p.Tracks)-1])
	return nil
}

func (r *PlaylistRepo) RemoveTrack(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	if r.ErrOnRemoveTrack != nil {
		return false, r.ErrOnRemoveTrack
	}
	p, err := r.ownedBy(playlistId, userId)
	if err != nil {
		return false, err
	}
	return r.removeTrack(p, trackId), nil
}

func (r *PlaylistRepo) RemoveTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
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

// removeTrack drops trackId from p, recording it in Removed when it was a member.
func (r *PlaylistRepo) removeTrack(p *domain.Playlist, trackId domain.TrackId) bool {
	if !p.RemoveTrack(trackId, p.UpdatedAt) {
		return false
	}
	r.Removed = append(r.Removed, trackId)
	return true
}

func (r *PlaylistRepo) ReorderTracks(_ context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
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

// coversExactly is the membership re-check the real adapter runs under its
// playlist lock: a plan built before a concurrent add or remove no longer
// names the playlist's tracks, and writing it would duplicate a position or
// leave a gap.
func coversExactly(planned, members []domain.PlaylistTrack) bool {
	return maps.Equal(trackIdSet(planned), trackIdSet(members))
}

func trackIdSet(tracks []domain.PlaylistTrack) map[domain.TrackId]bool {
	set := make(map[domain.TrackId]bool, len(tracks))
	for _, t := range tracks {
		set[t.TrackId] = true
	}
	return set
}

func (r *PlaylistRepo) Seed(playlist *domain.Playlist) {
	r.Playlists[playlist.ID.String()] = playlist
}

func (r *PlaylistRepo) SeedWithTracks(playlist *domain.Playlist, tracks []*domain.Track) {
	r.Playlists[playlist.ID.String()] = playlist
	r.PlaylistTracks[playlist.ID.String()] = tracks
}
