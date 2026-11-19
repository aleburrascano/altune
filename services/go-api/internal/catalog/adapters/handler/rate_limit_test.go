package handler_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	streamBurst        = 120
	streamEvery        = time.Second
	audioURLBurst      = 300
	audioURLEvery      = 250 * time.Millisecond
	trackWriteBurst    = 200
	trackWriteEvery    = 500 * time.Millisecond
	playlistWriteBurst = 60
	playlistWriteEvery = time.Second
)

type countingAudioStore struct {
	*catalog.InMemoryAudioStore
	mu       sync.Mutex
	streams  int
	presigns int
}

func (s *countingAudioStore) Stream(ctx context.Context, ref string) (catalog.AudioStream, int64, error) {
	s.mu.Lock()
	s.streams++
	s.mu.Unlock()
	return s.InMemoryAudioStore.Stream(ctx, ref)
}

func (s *countingAudioStore) PresignGet(_ context.Context, ref string, _ time.Duration) (string, error) {
	s.mu.Lock()
	s.presigns++
	s.mu.Unlock()
	return "https://storage.test/" + ref + "?sig=x", nil
}

const audioTestBytes = 64 * 1024

type audioRig struct {
	router chi.Router
	store  *countingAudioStore
	repo   *catalog.InMemoryTrackRepo
}

func newAudioRig(streamOpts []func(*catalog.StreamHandler), urlOpts []func(*catalog.AudioURLHandler)) *audioRig {
	repo := catalog.NewInMemoryTrackRepo()
	store := &countingAudioStore{InMemoryAudioStore: catalog.NewInMemoryAudioStore()}
	r := chi.NewRouter()
	r.Use(asBearer)
	catalog.NewStreamHandler(catalog.NewStreamTrackService(repo, store), streamOpts...).Routes(r)
	catalog.NewAudioURLHandler(catalog.NewAudioURLService(repo, store), urlOpts...).Routes(r)
	return &audioRig{router: r, store: store, repo: repo}
}

func (rig *audioRig) seedTracks(t *testing.T, user shared.UserId, n int) []*catalog.Track {
	t.Helper()
	tracks := make([]*catalog.Track, 0, n)
	for i := range n {
		ref := fmt.Sprintf("audio/%s-%d.opus", user.String(), i)
		tracks = append(tracks, catalog.SeedReadyTrack(t, rig.repo, user, fmt.Sprintf("Track %d", i), "Artist", "Album", ref))
		rig.store.Seed(ref, bytes.Repeat([]byte{0x5a}, audioTestBytes))
	}
	return tracks
}

func (rig *audioRig) stream(user shared.UserId, track *catalog.Track, rangeHdr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	return sendAs(rig.router, req, user)
}

func (rig *audioRig) resolve(user shared.UserId, tracks ...*catalog.Track) *httptest.ResponseRecorder {
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, `"`+t.ID.UUID().String()+`"`)
	}
	req := httptest.NewRequest(http.MethodPost, "/audio-urls", strings.NewReader(`{"track_ids":[`+strings.Join(ids, ",")+`]}`))
	req.Header.Set("Content-Type", "application/json")
	return sendAs(rig.router, req, user)
}

func assertAudioThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantRetryAfter string) {
	t.Helper()
	assertThrottled(t, rec, "catalog.audio_rate_limited", wantRetryAfter)
}

func assertThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantCode, wantRetryAfter string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+wantCode+`"`) {
		t.Fatalf("throttled body must carry the %s code, got %q", wantCode, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != wantRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, wantRetryAfter)
	}
}

func TestStreamRateLimit_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig([]func(*catalog.StreamHandler){catalog.WithStreamLimiterClock(clock.now)}, nil)
	user := shared.NewUserId(uuid.New())
	track := rig.seedTracks(t, user, 1)[0]

	for i := range streamBurst {
		if rec := rig.stream(user, track, ""); rec.Code != http.StatusOK {
			t.Fatalf("request %d inside the burst must stream, got %d", i, rec.Code)
		}
	}
	for range 50 {
		assertAudioThrottled(t, rig.stream(user, track, "bytes=0-1"), "1")
	}
	if rig.store.streams != streamBurst {
		t.Fatalf("throttled requests must not reach object storage: %d streams, want %d", rig.store.streams, streamBurst)
	}

	clock.advance(streamEvery)
	if rec := rig.stream(user, track, "bytes=0-1"); rec.Code != http.StatusPartialContent {
		t.Fatalf("a refilled token must admit the next range request, got %d", rec.Code)
	}
}

func TestAudioURLRateLimit_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig(nil, []func(*catalog.AudioURLHandler){catalog.WithAudioURLLimiterClock(clock.now)})
	user := shared.NewUserId(uuid.New())
	track := rig.seedTracks(t, user, 1)[0]

	for i := range audioURLBurst {
		if rec := rig.resolve(user, track); rec.Code != http.StatusOK {
			t.Fatalf("request %d inside the burst must resolve, got %d", i, rec.Code)
		}
	}
	for range 50 {
		assertAudioThrottled(t, rig.resolve(user, track), "1")
	}
	if rig.store.presigns != audioURLBurst {
		t.Fatalf("throttled requests must not presign: %d presigns, want %d", rig.store.presigns, audioURLBurst)
	}

	clock.advance(audioURLEvery)
	if rec := rig.resolve(user, track); rec.Code != http.StatusOK {
		t.Fatalf("a refilled token must admit the next resolve, got %d", rec.Code)
	}
}

func TestAudioRateLimit_IsPerPrincipalAndPerEndpoint(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig(
		[]func(*catalog.StreamHandler){catalog.WithStreamLimiterClock(clock.now)},
		[]func(*catalog.AudioURLHandler){catalog.WithAudioURLLimiterClock(clock.now)},
	)
	noisy, quiet := shared.NewUserId(uuid.New()), shared.NewUserId(uuid.New())
	noisyTrack := rig.seedTracks(t, noisy, 1)[0]
	quietTrack := rig.seedTracks(t, quiet, 1)[0]

	for range streamBurst {
		rig.stream(noisy, noisyTrack, "bytes=0-1")
	}
	assertAudioThrottled(t, rig.stream(noisy, noisyTrack, ""), "1")

	if rec := rig.resolve(noisy, noisyTrack); rec.Code != http.StatusOK {
		t.Fatalf("a spent stream budget must not throttle audio-urls, got %d", rec.Code)
	}
	if rec := rig.stream(quiet, quietTrack, ""); rec.Code != http.StatusOK {
		t.Fatalf("another user's stream budget must be untouched, got %d", rec.Code)
	}
}

func TestAudioRateLimit_UnauthenticatedStillGets401(t *testing.T) {
	rig := newAudioRig(nil, nil)

	stream := sendAnonymous(rig.router, httptest.NewRequest(http.MethodGet, "/tracks/"+uuid.NewString()+"/audio", nil))
	resolve := sendAnonymous(rig.router, httptest.NewRequest(http.MethodPost, "/audio-urls", strings.NewReader(`{"track_ids":[]}`)))

	if stream.Code != http.StatusUnauthorized || resolve.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal must still be a 401, got stream %d audio-urls %d", stream.Code, resolve.Code)
	}
}

func TestAudioRateLimit_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig(
		[]func(*catalog.StreamHandler){catalog.WithStreamLimiterClock(clock.now)},
		[]func(*catalog.AudioURLHandler){catalog.WithAudioURLLimiterClock(clock.now)},
	)
	user := shared.NewUserId(uuid.New())
	tracks := rig.seedTracks(t, user, 200)

	mustResolve := func(what string, batch ...*catalog.Track) {
		t.Helper()
		if rec := rig.resolve(user, batch...); rec.Code != http.StatusOK {
			t.Fatalf("%s throttled: %d %q", what, rec.Code, rec.Body.String())
		}
	}
	mustStream := func(what string, track *catalog.Track, rangeHdr string) {
		t.Helper()
		rec := rig.stream(user, track, rangeHdr)
		if rec.Code != http.StatusOK && rec.Code != http.StatusPartialContent {
			t.Fatalf("%s throttled: %d %q", what, rec.Code, rec.Body.String())
		}
	}

	mustResolve("queue-start presign window", tracks[:25]...)
	for i, track := range tracks {
		mustResolve(fmt.Sprintf("pin download %d", i), track)
		if i == 100 {
			mustResolve("prefetch during pin drain", tracks[1])
		}
	}

	for i := range 20 {
		track := tracks[i]
		mustStream("skip probe", track, "bytes=0-1")
		mustStream("skip read", track, "bytes=0-16383")
		mustStream("skip read", track, "bytes=16384-")
		mustResolve("skip prefetch", tracks[i+1])
		clock.advance(500 * time.Millisecond)
	}

	const trackLen = 3 * time.Minute
	for i := range 40 {
		track := tracks[20+i]
		mustStream("track start probe", track, "bytes=0-1")
		mustResolve("prefetch next", tracks[21+i])
		if i%20 == 0 {
			mustResolve("presign window slide", tracks[21+i:46+i]...)
		}
		for r := range 6 {
			mustStream("buffer refill", track, fmt.Sprintf("bytes=%d-", r*8192))
		}
		for s := range 10 {
			mustStream("seek", track, fmt.Sprintf("bytes=%d-", (s+1)*4096))
		}
		clock.advance(trackLen)
	}
}

type writeRig struct {
	router    chi.Router
	tracks    *catalog.InMemoryTrackRepo
	playlists *catalog.InMemoryPlaylistRepo
}

func newWriteRig(trackOpts []func(*catalog.TrackHandler), playlistOpts []func(*catalog.PlaylistHandler)) *writeRig {
	tracks := catalog.NewInMemoryTrackRepo()
	playlists := catalog.NewInMemoryPlaylistRepo()
	playlistH := catalog.NewPlaylistHandler(
		catalog.NewPlaylistLifecycleService(playlists),
		catalog.NewPlaylistMembershipService(playlists, tracks),
		playlistOpts...,
	)
	r := trackRoutes(tracks, trackOpts...)
	r.Mount("/playlists", playlistH.Routes())
	return &writeRig{router: r, tracks: tracks, playlists: playlists}
}

func (rig *writeRig) post(t *testing.T, user shared.UserId, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return sendAs(rig.router, jsonRequest(t, http.MethodPost, path, body), user)
}

func (rig *writeRig) createTrack(t *testing.T, user shared.UserId, title string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/tracks/", map[string]string{"title": title, "artist": "Artist", "album": "Album"})
}

func (rig *writeRig) createPlaylist(t *testing.T, user shared.UserId, name string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/", map[string]string{"name": name})
}

func TestWriteRateLimits_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newFakeClock()
	rig := newWriteRig(
		[]func(*catalog.TrackHandler){catalog.WithTrackWriteLimiterClock(clock.now)},
		[]func(*catalog.PlaylistHandler){catalog.WithPlaylistWriteLimiterClock(clock.now)},
	)
	user := shared.NewUserId(uuid.New())
	playlist := catalog.SeedPlaylist(t, rig.playlists, user, "Seeded")
	track := catalog.SeedTrack(t, rig.tracks, user, "Seeded Track", "Artist", "Album")

	for i := range 150 {
		if rec := rig.createTrack(t, user, fmt.Sprintf("Compilation Track %d", i)); rec.Code != http.StatusCreated {
			t.Fatalf("save-all track %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	batch := map[string][]string{"track_ids": {track.ID.String()}}
	for i := range 40 {
		if rec := rig.post(t, user, "/playlists/"+playlist.ID.String()+"/tracks/batch", batch); rec.Code != http.StatusOK {
			t.Fatalf("add-to-playlist %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
}

type limitedEndpoint struct {
	name       string
	burst      int
	every      time.Duration
	code       string
	retryAfter string
	open       func(t *testing.T, clocks ...func() time.Time) func(i int) *httptest.ResponseRecorder
}

func openStream(t *testing.T, clocks ...func() time.Time) func(int) *httptest.ResponseRecorder {
	var opts []func(*catalog.StreamHandler)
	for _, clock := range clocks {
		opts = append(opts, catalog.WithStreamLimiterClock(clock))
	}
	rig := newAudioRig(opts, nil)
	track := rig.seedTracks(t, catalog.FixtureUserId(), 1)[0]
	return func(int) *httptest.ResponseRecorder { return rig.stream(catalog.FixtureUserId(), track, "bytes=0-1") }
}

func openAudioURLs(t *testing.T, clocks ...func() time.Time) func(int) *httptest.ResponseRecorder {
	var opts []func(*catalog.AudioURLHandler)
	for _, clock := range clocks {
		opts = append(opts, catalog.WithAudioURLLimiterClock(clock))
	}
	rig := newAudioRig(nil, opts)
	track := rig.seedTracks(t, catalog.FixtureUserId(), 1)[0]
	return func(int) *httptest.ResponseRecorder { return rig.resolve(catalog.FixtureUserId(), track) }
}

func openTrackWrites(t *testing.T, clocks ...func() time.Time) func(int) *httptest.ResponseRecorder {
	var opts []func(*catalog.TrackHandler)
	for _, clock := range clocks {
		opts = append(opts, catalog.WithTrackWriteLimiterClock(clock))
	}
	rig := newWriteRig(opts, nil)
	return func(i int) *httptest.ResponseRecorder {
		return rig.createTrack(t, catalog.FixtureUserId(), fmt.Sprintf("Track %d", i))
	}
}

func openPlaylistWrites(t *testing.T, clocks ...func() time.Time) func(int) *httptest.ResponseRecorder {
	var opts []func(*catalog.PlaylistHandler)
	for _, clock := range clocks {
		opts = append(opts, catalog.WithPlaylistWriteLimiterClock(clock))
	}
	rig := newWriteRig(nil, opts)
	return func(i int) *httptest.ResponseRecorder {
		return rig.createPlaylist(t, catalog.FixtureUserId(), fmt.Sprintf("Playlist %d", i))
	}
}

var limitedEndpoints = []limitedEndpoint{
	{"stream", streamBurst, streamEvery, "catalog.audio_rate_limited", "1", openStream},
	{"audio-urls", audioURLBurst, audioURLEvery, "catalog.audio_rate_limited", "1", openAudioURLs},
	{"track writes", trackWriteBurst, trackWriteEvery, "catalog.write_rate_limited", "1", openTrackWrites},
	{"playlist writes", playlistWriteBurst, playlistWriteEvery, "catalog.write_rate_limited", "1", openPlaylistWrites},
}

func assertDefaultBoundary(t *testing.T, ep limitedEndpoint, clock *fakeClock, hit func(int) *httptest.ResponseRecorder) {
	t.Helper()
	for i := range ep.burst {
		if rec := hit(i); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of the default burst of %d was throttled", i+1, ep.burst)
		}
	}
	assertThrottled(t, hit(ep.burst), ep.code, ep.retryAfter)
	clock.advance(ep.every - time.Millisecond)
	assertThrottled(t, hit(ep.burst+1), ep.code, ep.retryAfter)
	clock.advance(time.Millisecond)
	if rec := hit(ep.burst + 2); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("one default interval of %s must refill a token, still throttled", ep.every)
	}
}

func TestRateLimits_DefaultsHoldAtTheirBoundary(t *testing.T) {
	for _, ep := range limitedEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			clock := newFakeClock()
			assertDefaultBoundary(t, ep, clock, ep.open(t, clock.now))
		})
	}
}

func TestRateLimits_ANilClockKeepsTheClockAlreadySet(t *testing.T) {
	for _, ep := range limitedEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			clock := newFakeClock()
			assertDefaultBoundary(t, ep, clock, ep.open(t, clock.now, nil))
		})
	}
}
