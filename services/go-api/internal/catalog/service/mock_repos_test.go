package service

import (
	"bytes"
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

// --- mockTrackRepo ---

type mockTrackRepo struct {
	tracks map[string]*domain.Track // keyed by trackId.String()

	errOnAdd    error
	errOnGetBy  error
	errOnList   error
	errOnUpdate error
	errOnDelete error

	addReturnsCreated *bool // override the default dedup-based created flag
}

func newMockTrackRepo() *mockTrackRepo {
	return &mockTrackRepo{tracks: make(map[string]*domain.Track)}
}

func (r *mockTrackRepo) Add(_ context.Context, track *domain.Track) (bool, error) {
	if r.errOnAdd != nil {
		return false, r.errOnAdd
	}
	// Dedup by DedupKey+UserId
	for _, t := range r.tracks {
		if t.DedupKey == track.DedupKey && t.UserId == track.UserId {
			if r.addReturnsCreated != nil {
				return *r.addReturnsCreated, nil
			}
			return false, nil
		}
	}
	r.tracks[track.ID.String()] = track
	return true, nil
}

func (r *mockTrackRepo) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if r.errOnGetBy != nil {
		return nil, r.errOnGetBy
	}
	// Look up by dedup key match first (for Add's duplicate path), then by exact ID
	for _, t := range r.tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *mockTrackRepo) ListForUser(_ context.Context, userId shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	if r.errOnList != nil {
		return nil, 0, r.errOnList
	}
	var all []*domain.Track
	for _, t := range r.tracks {
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

func (r *mockTrackRepo) Update(_ context.Context, track *domain.Track) error {
	if r.errOnUpdate != nil {
		return r.errOnUpdate
	}
	r.tracks[track.ID.String()] = track
	return nil
}

func (r *mockTrackRepo) Delete(_ context.Context, id domain.TrackId, userId shared.UserId) (bool, error) {
	if r.errOnDelete != nil {
		return false, r.errOnDelete
	}
	key := id.String()
	t, ok := r.tracks[key]
	if !ok || t.UserId != userId {
		return false, nil
	}
	delete(r.tracks, key)
	return true, nil
}

func (r *mockTrackRepo) GetByDedupKey(_ context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error) {
	if r.errOnGetBy != nil {
		return nil, r.errOnGetBy
	}
	for _, t := range r.tracks {
		if t.DedupKey == dedupKey && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

// seed adds a track directly into the mock store.
func (r *mockTrackRepo) seed(track *domain.Track) {
	r.tracks[track.ID.String()] = track
}

// --- mockPlaylistRepo ---

type mockPlaylistRepo struct {
	playlists map[string]*domain.Playlist // keyed by playlistId.String()
	// For GetWithTracks, tracks are stored separately
	playlistTracks map[string][]*domain.Track // keyed by playlistId.String()

	errOnCreate       error
	errOnGetByID      error
	errOnGetWithTracks error
	errOnList         error
	errOnDelete       error
	errOnUpdate       error
	errOnAddTrack     error
	errOnRemoveTrack  error
	errOnReorder      error

	deleteReturns *bool // override default behavior
}

func newMockPlaylistRepo() *mockPlaylistRepo {
	return &mockPlaylistRepo{
		playlists:      make(map[string]*domain.Playlist),
		playlistTracks: make(map[string][]*domain.Track),
	}
}

func (r *mockPlaylistRepo) Create(_ context.Context, playlist *domain.Playlist) error {
	if r.errOnCreate != nil {
		return r.errOnCreate
	}
	r.playlists[playlist.ID.String()] = playlist
	return nil
}

func (r *mockPlaylistRepo) ListForUser(_ context.Context, userId shared.UserId) ([]*domain.Playlist, error) {
	if r.errOnList != nil {
		return nil, r.errOnList
	}
	var result []*domain.Playlist
	for _, p := range r.playlists {
		if p.UserId == userId {
			result = append(result, p)
		}
	}
	return result, nil
}

func (r *mockPlaylistRepo) GetByID(_ context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error) {
	if r.errOnGetByID != nil {
		return nil, r.errOnGetByID
	}
	p, ok := r.playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil
	}
	return p, nil
}

func (r *mockPlaylistRepo) GetWithTracks(_ context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error) {
	if r.errOnGetWithTracks != nil {
		return nil, nil, r.errOnGetWithTracks
	}
	p, ok := r.playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil, nil
	}
	return p, r.playlistTracks[id.String()], nil
}

func (r *mockPlaylistRepo) Delete(_ context.Context, id domain.PlaylistId, userId shared.UserId) (bool, error) {
	if r.errOnDelete != nil {
		return false, r.errOnDelete
	}
	if r.deleteReturns != nil {
		return *r.deleteReturns, nil
	}
	key := id.String()
	p, ok := r.playlists[key]
	if !ok || p.UserId != userId {
		return false, nil
	}
	delete(r.playlists, key)
	return true, nil
}

func (r *mockPlaylistRepo) Update(_ context.Context, playlist *domain.Playlist) error {
	if r.errOnUpdate != nil {
		return r.errOnUpdate
	}
	r.playlists[playlist.ID.String()] = playlist
	return nil
}

func (r *mockPlaylistRepo) AddTrack(_ context.Context, _ domain.PlaylistId, _ domain.TrackId, _ int) error {
	if r.errOnAddTrack != nil {
		return r.errOnAddTrack
	}
	return nil
}

func (r *mockPlaylistRepo) RemoveTrack(_ context.Context, _ domain.PlaylistId, _ domain.TrackId) error {
	if r.errOnRemoveTrack != nil {
		return r.errOnRemoveTrack
	}
	return nil
}

func (r *mockPlaylistRepo) ReorderTracks(_ context.Context, _ domain.PlaylistId, _ []domain.PlaylistTrack) error {
	if r.errOnReorder != nil {
		return r.errOnReorder
	}
	return nil
}

func (r *mockPlaylistRepo) GetPreviewArtwork(_ context.Context, _ domain.PlaylistId) ([]string, error) {
	return nil, nil
}

// seed adds a playlist directly into the mock store.
func (r *mockPlaylistRepo) seed(playlist *domain.Playlist) {
	r.playlists[playlist.ID.String()] = playlist
}

// seedWithTracks adds a playlist and its associated tracks for GetWithTracks.
func (r *mockPlaylistRepo) seedWithTracks(playlist *domain.Playlist, tracks []*domain.Track) {
	r.playlists[playlist.ID.String()] = playlist
	r.playlistTracks[playlist.ID.String()] = tracks
}

// --- mockAudioStore ---

type mockAudioStore struct {
	files map[string]bool // audioRef -> exists

	errOnExists error
	errOnStore  error
	errOnStream error
	errOnDelete error
}

func newMockAudioStore() *mockAudioStore {
	return &mockAudioStore{files: make(map[string]bool)}
}

func (s *mockAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.errOnExists != nil {
		return false, s.errOnExists
	}
	return s.files[audioRef], nil
}

func (s *mockAudioStore) Store(_ context.Context, _ string, audioRef string) error {
	if s.errOnStore != nil {
		return s.errOnStore
	}
	s.files[audioRef] = true
	return nil
}

func (s *mockAudioStore) Stream(_ context.Context, _ string) (ports.AudioStream, int64, error) {
	if s.errOnStream != nil {
		return nil, 0, s.errOnStream
	}
	return nopAudioStream{bytes.NewReader(nil)}, 0, nil
}

type nopAudioStream struct{ *bytes.Reader }

func (nopAudioStream) Close() error { return nil }

func (s *mockAudioStore) Delete(_ context.Context, audioRef string) error {
	if s.errOnDelete != nil {
		return s.errOnDelete
	}
	delete(s.files, audioRef)
	return nil
}

// seed marks an audioRef as existing in the store.
func (s *mockAudioStore) seed(audioRef string) {
	s.files[audioRef] = true
}
