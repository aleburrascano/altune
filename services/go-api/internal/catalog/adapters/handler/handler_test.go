package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/auth"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// --- test constants ---

var (
	testUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	testUserId   = shared.NewUserId(testUserUUID)
)

// --- fake token verifier ---

// verifyAsTestUser always succeeds and returns testUserId.
var verifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
	return testUserId, nil
})

// --- fake track repo ---

type fakeTrackRepo struct {
	tracks map[string]*catdomain.Track

	addCreated bool
	addErr     error
	getErr     error
	listErr    error
	updateErr  error
	deleteErr  error
	deletedOk  bool
}

func newFakeTrackRepo() *fakeTrackRepo {
	return &fakeTrackRepo{tracks: make(map[string]*catdomain.Track), deletedOk: true}
}

func (r *fakeTrackRepo) Add(_ context.Context, track *catdomain.Track) (*catdomain.Track, bool, error) {
	if r.addErr != nil {
		return nil, false, r.addErr
	}
	for _, t := range r.tracks {
		if t.DedupKey == track.DedupKey && t.UserId == track.UserId {
			return t, false, nil
		}
	}
	r.tracks[track.ID.String()] = track
	r.addCreated = true
	return track, true, nil
}

func (r *fakeTrackRepo) GetByID(_ context.Context, id catdomain.TrackId, userId shared.UserId) (*catdomain.Track, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, t := range r.tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *fakeTrackRepo) ListForUser(_ context.Context, userId shared.UserId, limit, offset int) ([]*catdomain.Track, int, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	var all []*catdomain.Track
	for _, t := range r.tracks {
		if t.UserId == userId {
			all = append(all, t)
		}
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (r *fakeTrackRepo) ListByIDs(_ context.Context, userId shared.UserId, ids []catdomain.TrackId) ([]*catdomain.Track, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*catdomain.Track
	for _, id := range ids {
		if t, ok := r.tracks[id.String()]; ok && t.UserId == userId {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *fakeTrackRepo) Update(_ context.Context, track *catdomain.Track) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.tracks[track.ID.String()] = track
	return nil
}

func (r *fakeTrackRepo) SetTrackNumber(_ context.Context, id catdomain.TrackId, _ shared.UserId, n int) (bool, error) {
	t, ok := r.tracks[id.String()]
	if !ok || t.TrackNumber != nil {
		return false, nil
	}
	t.TrackNumber = &n
	return true, nil
}

func (r *fakeTrackRepo) Delete(_ context.Context, id catdomain.TrackId, userId shared.UserId) (bool, error) {
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	key := id.String()
	t, ok := r.tracks[key]
	if !ok || t.UserId != userId {
		return false, nil
	}
	delete(r.tracks, key)
	return r.deletedOk, nil
}

func (r *fakeTrackRepo) GetByDedupKey(_ context.Context, userId shared.UserId, dedupKey string) (*catdomain.Track, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, t := range r.tracks {
		if t.DedupKey == dedupKey && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *fakeTrackRepo) ReplaceFeaturedArtists(_ context.Context, id catdomain.TrackId, userId shared.UserId, feats []catdomain.FeaturedArtist) error {
	if t, ok := r.tracks[id.String()]; ok && t.UserId == userId {
		t.FeaturedArtists = feats
	}
	return nil
}

func (r *fakeTrackRepo) ListTracksFeaturing(_ context.Context, userId shared.UserId, fa catdomain.FeaturedArtist) ([]*catdomain.Track, error) {
	var out []*catdomain.Track
	for _, t := range r.tracks {
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

func (r *fakeTrackRepo) seed(t *catdomain.Track) {
	r.tracks[t.ID.String()] = t
}

// --- fake playlist repo ---

type fakePlaylistRepo struct {
	playlists      map[string]*catdomain.Playlist
	playlistTracks map[string][]*catdomain.Track

	createErr       error
	getByIDErr      error
	getWithTracksErr error
	listErr         error
	deleteErr       error
	updateErr       error
	addTrackErr     error
	removeTrackErr  error
	reorderErr      error
	deletedOk       bool
}

func newFakePlaylistRepo() *fakePlaylistRepo {
	return &fakePlaylistRepo{
		playlists:      make(map[string]*catdomain.Playlist),
		playlistTracks: make(map[string][]*catdomain.Track),
		deletedOk:      true,
	}
}

func (r *fakePlaylistRepo) Create(_ context.Context, pl *catdomain.Playlist) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.playlists[pl.ID.String()] = pl
	return nil
}

func (r *fakePlaylistRepo) ListForUser(_ context.Context, userId shared.UserId) ([]*catdomain.Playlist, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var res []*catdomain.Playlist
	for _, p := range r.playlists {
		if p.UserId == userId {
			res = append(res, p)
		}
	}
	return res, nil
}

func (r *fakePlaylistRepo) GetByID(_ context.Context, id catdomain.PlaylistId, userId shared.UserId) (*catdomain.Playlist, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	p, ok := r.playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil
	}
	return p, nil
}

func (r *fakePlaylistRepo) GetWithTracks(_ context.Context, id catdomain.PlaylistId, userId shared.UserId) (*catdomain.Playlist, []*catdomain.Track, error) {
	if r.getWithTracksErr != nil {
		return nil, nil, r.getWithTracksErr
	}
	p, ok := r.playlists[id.String()]
	if !ok || p.UserId != userId {
		return nil, nil, nil
	}
	return p, r.playlistTracks[id.String()], nil
}

func (r *fakePlaylistRepo) Delete(_ context.Context, id catdomain.PlaylistId, userId shared.UserId) (bool, error) {
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	key := id.String()
	p, ok := r.playlists[key]
	if !ok || p.UserId != userId {
		return false, nil
	}
	delete(r.playlists, key)
	return r.deletedOk, nil
}

func (r *fakePlaylistRepo) Update(_ context.Context, pl *catdomain.Playlist) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.playlists[pl.ID.String()] = pl
	return nil
}

func (r *fakePlaylistRepo) AddTrack(_ context.Context, _ catdomain.PlaylistId, _ catdomain.TrackId, _ int) error {
	return r.addTrackErr
}

func (r *fakePlaylistRepo) RemoveTrack(_ context.Context, _ catdomain.PlaylistId, _ catdomain.TrackId) error {
	return r.removeTrackErr
}

func (r *fakePlaylistRepo) ReorderTracks(_ context.Context, _ catdomain.PlaylistId, _ []catdomain.PlaylistTrack) error {
	return r.reorderErr
}

func (r *fakePlaylistRepo) seed(pl *catdomain.Playlist) {
	r.playlists[pl.ID.String()] = pl
}

func (r *fakePlaylistRepo) seedWithTracks(pl *catdomain.Playlist, tracks []*catdomain.Track) {
	r.playlists[pl.ID.String()] = pl
	r.playlistTracks[pl.ID.String()] = tracks
}

// --- fake audio store ---

type fakeAudioStore struct {
	data      map[string][]byte
	existsErr error
	storeErr  error
	streamErr error
	deleteErr error
}

func newFakeAudioStore() *fakeAudioStore {
	return &fakeAudioStore{data: make(map[string][]byte)}
}

func (s *fakeAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	_, ok := s.data[audioRef]
	return ok, nil
}

func (s *fakeAudioStore) Store(_ context.Context, _ string, audioRef string) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	s.data[audioRef] = []byte("audio-data")
	return nil
}

func (s *fakeAudioStore) Stream(_ context.Context, audioRef string) (ports.AudioStream, int64, error) {
	if s.streamErr != nil {
		return nil, 0, s.streamErr
	}
	data, ok := s.data[audioRef]
	if !ok {
		return nil, 0, io.EOF
	}
	return fakeAudioStream{bytes.NewReader(data)}, int64(len(data)), nil
}

type fakeAudioStream struct{ *bytes.Reader }

func (fakeAudioStream) Close() error { return nil }

func (s *fakeAudioStore) Delete(_ context.Context, audioRef string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, audioRef)
	return nil
}

func (s *fakeAudioStore) seed(audioRef string, data []byte) {
	s.data[audioRef] = data
}

// --- fake acquisition scheduler ---

type fakeScheduler struct {
	scheduled  []catdomain.TrackId
	sourceURLs []string
}

func (s *fakeScheduler) Schedule(_ shared.UserId, trackId catdomain.TrackId, sourceURL string) {
	s.scheduled = append(s.scheduled, trackId)
	s.sourceURLs = append(s.sourceURLs, sourceURL)
}

// serve sends a request through a chi router and returns the response recorder.
func serve(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// serveNoAuth sends a request without an Authorization header.
func serveNoAuth(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// jsonBody encodes v as JSON for use as a request body.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return buf
}

// decodeJSON decodes the response body into dst.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, rec.Body.String())
	}
}

// --- domain helpers ---

func makeTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, _ := catdomain.NewTrack(userId, title, artist, album)
	return t
}

func makeReadyTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func makePlaylist(userId shared.UserId, name string) *catdomain.Playlist {
	p, _ := catdomain.NewPlaylist(userId, name)
	return p
}

// --- service builders ---

func buildTrackHandler(trackRepo *fakeTrackRepo, scheduler *fakeScheduler) (*TrackHandler, chi.Router) {
	var addOpts []func(*service.AddTrackService)
	if scheduler != nil {
		addOpts = append(addOpts, service.WithAcquisitionScheduler(scheduler))
	}
	addSvc := service.NewAddTrackService(trackRepo, addOpts...)
	listSvc := service.NewListTracksService(trackRepo)
	deleteSvc := service.NewDeleteTrackService(trackRepo, newFakeAudioStore())
	setTrackNumberSvc := service.NewSetTrackNumberService(trackRepo)

	backfillSvc := service.NewBackfillFeaturedService(trackRepo, nil)
	listFeaturingSvc := service.NewListFeaturingService(trackRepo)
	h := NewTrackHandler(addSvc, listSvc, deleteSvc, setTrackNumberSvc, backfillSvc, listFeaturingSvc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Mount("/tracks", h.Routes())
	return h, r
}

func buildPlaylistHandler(plRepo *fakePlaylistRepo, trRepo *fakeTrackRepo) (*PlaylistHandler, chi.Router) {
	svc := service.NewPlaylistService(plRepo, trRepo)
	h := NewPlaylistHandler(svc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Mount("/playlists", h.Routes())
	return h, r
}

func buildStreamHandler(trackRepo *fakeTrackRepo, audioStore *fakeAudioStore, scheduler *fakeScheduler) (*StreamHandler, chi.Router) {
	var sched ports.AcquisitionScheduler
	if scheduler != nil {
		sched = scheduler
	}
	streamSvc := service.NewStreamTrackService(trackRepo, audioStore, sched)
	h := NewStreamHandler(streamSvc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Get("/tracks/{trackId}/stream", h.HandleStreamAudio)
	return h, r
}

// assertStatus checks the HTTP status code.
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

// assertJSON checks that the response has application/json content type.
func assertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
