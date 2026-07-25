package catalogtest

import (
	"bytes"
	"context"
	"io"
	"strings"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type TrackRepo struct {
	Tracks map[string]*domain.Track

	ErrOnAdd    error
	ErrOnGetBy  error
	ErrOnList   error
	ErrOnUpdate error
	ErrOnDelete error
}

func NewTrackRepo() *TrackRepo {
	return &TrackRepo{Tracks: make(map[string]*domain.Track)}
}

func (r *TrackRepo) Add(_ context.Context, track *domain.Track) (*domain.Track, bool, error) {
	if r.ErrOnAdd != nil {
		return nil, false, r.ErrOnAdd
	}
	for _, t := range r.Tracks {
		if t.DedupKey == track.DedupKey && t.UserId == track.UserId {
			return t, false, nil
		}
	}
	r.Tracks[track.ID.String()] = track
	return track, true, nil
}

func (r *TrackRepo) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *TrackRepo) ListForUser(_ context.Context, userId shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	if r.ErrOnList != nil {
		return nil, 0, r.ErrOnList
	}
	var all []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId == userId {
			all = append(all, t)
		}
	}
	total := len(all)
	if offset >= len(all) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (r *TrackRepo) ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]*domain.Track, int, error) {
	tracks, total, err := r.ListForUser(ctx, userId, query.Limit, query.Offset)
	if err != nil || query.Search == "" {
		return tracks, total, err
	}
	needle := strings.ToLower(query.Search)
	var matched []*domain.Track
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title), needle) ||
			strings.Contains(strings.ToLower(t.Artist), needle) ||
			strings.Contains(strings.ToLower(t.Album), needle) {
			matched = append(matched, t)
		}
	}
	return matched, len(matched), nil
}

func (r *TrackRepo) ListAlbumsForUser(_ context.Context, userId shared.UserId, _ domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.AlbumGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId != userId || t.Album == "" {
			continue
		}
		artist := t.Artist
		if t.AlbumArtist != nil {
			artist = *t.AlbumArtist
		}
		key := strings.ToLower(t.Album) + "|||" + strings.ToLower(artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = &domain.AlbumGroup{
			Key: key, Album: t.Album, Artist: artist,
			ArtworkURL: t.ArtworkURL, Year: t.Year,
			TrackCount: 1, MostRecentAddedAt: t.AddedAt,
		}
		order = append(order, key)
	}
	out := make([]domain.AlbumGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func (r *TrackRepo) ListArtistsForUser(_ context.Context, userId shared.UserId, _ domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.ArtistGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId != userId {
			continue
		}
		key := strings.ToLower(t.Artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = &domain.ArtistGroup{
			Key: key, Artist: t.Artist, ArtworkURL: t.ArtworkURL,
			TrackCount: 1, MostRecentAddedAt: t.AddedAt,
		}
		order = append(order, key)
	}
	out := make([]domain.ArtistGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func (r *TrackRepo) ListByIDs(_ context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	var out []*domain.Track
	for _, id := range ids {
		if t, ok := r.Tracks[id.String()]; ok && t.UserId == userId {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *TrackRepo) Update(_ context.Context, track *domain.Track) error {
	if r.ErrOnUpdate != nil {
		return r.ErrOnUpdate
	}
	r.Tracks[track.ID.String()] = track
	return nil
}

func (r *TrackRepo) SetTrackNumber(_ context.Context, id domain.TrackId, _ shared.UserId, n int) (bool, error) {
	t, ok := r.Tracks[id.String()]
	if !ok || t.TrackNumber != nil {
		return false, nil
	}
	t.TrackNumber = &n
	return true, nil
}

func (r *TrackRepo) Delete(_ context.Context, id domain.TrackId, userId shared.UserId) (bool, *string, error) {
	if r.ErrOnDelete != nil {
		return false, nil, r.ErrOnDelete
	}
	key := id.String()
	t, ok := r.Tracks[key]
	if !ok || t.UserId != userId {
		return false, nil, nil
	}
	audioRef := t.AudioRef
	delete(r.Tracks, key)
	return true, audioRef, nil
}

func (r *TrackRepo) GetByDedupKey(_ context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.DedupKey == dedupKey && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *TrackRepo) ReplaceFeaturedArtists(_ context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error {
	t, ok := r.Tracks[id.String()]
	if !ok || t.UserId != userId {
		return nil
	}
	t.FeaturedArtists = feats
	return nil
}

func (r *TrackRepo) ListTracksFeaturing(_ context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error) {
	var out []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId != userId {
			continue
		}
		for _, f := range t.FeaturedArtists {
			if f.IdentityKey() == fa.IdentityKey() {
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}

func (r *TrackRepo) Seed(track *domain.Track) {
	r.Tracks[track.ID.String()] = track
}

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
	ErrOnRemoveTrack   error
	ErrOnReorder       error
}

var _ ports.PlaylistRepository = (*PlaylistRepo)(nil)

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

func (r *PlaylistRepo) AddTrack(_ context.Context, _ domain.PlaylistId, _ domain.TrackId, _ int) error {
	return r.ErrOnAddTrack
}

func (r *PlaylistRepo) RemoveTrack(_ context.Context, _ domain.PlaylistId, _ domain.TrackId) error {
	return r.ErrOnRemoveTrack
}

func (r *PlaylistRepo) ReorderTracks(_ context.Context, _ domain.PlaylistId, _ []domain.PlaylistTrack) error {
	return r.ErrOnReorder
}

func (r *PlaylistRepo) Seed(playlist *domain.Playlist) {
	r.Playlists[playlist.ID.String()] = playlist
}

func (r *PlaylistRepo) SeedWithTracks(playlist *domain.Playlist, tracks []*domain.Track) {
	r.Playlists[playlist.ID.String()] = playlist
	r.PlaylistTracks[playlist.ID.String()] = tracks
}

type AudioStore struct {
	Files map[string][]byte

	ErrOnExists error
	ErrOnStore  error
	ErrOnStream error
	ErrOnDelete error
}

var _ ports.AudioStore = (*AudioStore)(nil)

func NewAudioStore() *AudioStore {
	return &AudioStore{Files: make(map[string][]byte)}
}

func (s *AudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.ErrOnExists != nil {
		return false, s.ErrOnExists
	}
	_, ok := s.Files[audioRef]
	return ok, nil
}

func (s *AudioStore) Store(_ context.Context, _ string, audioRef string) error {
	if s.ErrOnStore != nil {
		return s.ErrOnStore
	}
	s.Files[audioRef] = []byte("audio-data")
	return nil
}

func (s *AudioStore) Stream(_ context.Context, audioRef string) (ports.AudioStream, int64, error) {
	if s.ErrOnStream != nil {
		return nil, 0, s.ErrOnStream
	}
	data, ok := s.Files[audioRef]
	if !ok {
		return nil, 0, io.EOF
	}
	return audioStream{bytes.NewReader(data)}, int64(len(data)), nil
}

type audioStream struct{ *bytes.Reader }

func (audioStream) Close() error { return nil }

func (s *AudioStore) Delete(_ context.Context, audioRef string) error {
	if s.ErrOnDelete != nil {
		return s.ErrOnDelete
	}
	delete(s.Files, audioRef)
	return nil
}

func (s *AudioStore) Seed(audioRef string, data []byte) {
	s.Files[audioRef] = data
}

type Scheduler struct {
	TrackIds   []domain.TrackId
	SourceURLs []string
}

var _ ports.AcquisitionScheduler = (*Scheduler)(nil)

func (s *Scheduler) Schedule(_ shared.UserId, trackId domain.TrackId, sourceURL string) {
	s.TrackIds = append(s.TrackIds, trackId)
	s.SourceURLs = append(s.SourceURLs, sourceURL)
}
