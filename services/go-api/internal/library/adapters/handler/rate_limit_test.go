package handler_test

import (
	"altune/go-api/internal/library"
	sharedids "altune/go-api/internal/shared/ids"
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
	*library.InMemoryAudioStore
	mu       sync.Mutex
	streams  int
	presigns int
}

func (s *countingAudioStore) Stream(ctx context.Context, ref string) (library.AudioStream, int64, error) {
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
	repo   *library.InMemoryTrackRepo
}

func newAudioRig(streamOpts []func(*library.StreamHandler), urlOpts []func(*library.AudioURLHandler)) *audioRig {
	repo := library.NewInMemoryTrackRepo()
	store := &countingAudioStore{InMemoryAudioStore: library.NewInMemoryAudioStore()}
	r := chi.NewRouter()
	r.Use(asBearer)
	library.NewStreamHandler(library.NewStreamTrackService(repo, store), streamOpts...).Routes(r)
	library.NewAudioURLHandler(library.NewAudioURLService(repo, store), urlOpts...).Routes(r)
	return &audioRig{router: r, store: store, repo: repo}
}

func (rig *audioRig) seedTracks(t *testing.T, user sharedids.UserId, n int) []*library.Track {
	t.Helper()
	tracks := make([]*library.Track, 0, n)
	for i := range n {
		ref := fmt.Sprintf("audio/%s-%d.opus", user.String(), i)
		tracks = append(tracks, library.SeedReadyTrack(t, rig.repo, user, fmt.Sprintf("Track %d", i), "Artist", "Album", ref))
		rig.store.Seed(ref, bytes.Repeat([]byte{0x5a}, audioTestBytes))
	}
	return tracks
}

func (rig *audioRig) stream(user sharedids.UserId, track *library.Track, rangeHdr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	return sendAs(rig.router, req, user)
}

func (rig *audioRig) resolve(user sharedids.UserId, tracks ...*library.Track) *httptest.ResponseRecorder {
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

func TestAudioRateLimit_IsPerPrincipalAndPerEndpoint(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig(
		[]func(*library.StreamHandler){library.WithStreamLimiterClock(clock.now)},
		[]func(*library.AudioURLHandler){library.WithAudioURLLimiterClock(clock.now)},
	)
	noisy, quiet := sharedids.NewUserId(uuid.New()), sharedids.NewUserId(uuid.New())
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
		[]func(*library.StreamHandler){library.WithStreamLimiterClock(clock.now)},
		[]func(*library.AudioURLHandler){library.WithAudioURLLimiterClock(clock.now)},
	)
	user := sharedids.NewUserId(uuid.New())
	tracks := rig.seedTracks(t, user, 200)

	mustResolve := func(what string, batch ...*library.Track) {
		t.Helper()
		if rec := rig.resolve(user, batch...); rec.Code != http.StatusOK {
			t.Fatalf("%s throttled: %d %q", what, rec.Code, rec.Body.String())
		}
	}
	mustStream := func(what string, track *library.Track, rangeHdr string) {
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

func TestWriteRateLimits_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newFakeClock()
	rig := newWriteRig(
		[]func(*library.TrackHandler){library.WithTrackWriteLimiterClock(clock.now)},
		[]func(*library.PlaylistHandler){library.WithPlaylistWriteLimiterClock(clock.now)},
	)
	user := sharedids.NewUserId(uuid.New())
	playlist := library.SeedPlaylist(t, rig.playlists, user, "Seeded")
	track := library.SeedTrack(t, rig.tracks, user, "Seeded Track", "Artist", "Album")

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
	open       func(t *testing.T, clocks ...func() time.Time) endpointProbe
}

type endpointProbe struct {
	hit     func(i int) *httptest.ResponseRecorder
	reached func() int
}

func openStream(t *testing.T, clocks ...func() time.Time) endpointProbe {
	var opts []func(*library.StreamHandler)
	for _, clock := range clocks {
		opts = append(opts, library.WithStreamLimiterClock(clock))
	}
	rig := newAudioRig(opts, nil)
	track := rig.seedTracks(t, library.FixtureUserId(), 1)[0]
	return endpointProbe{
		hit:     func(int) *httptest.ResponseRecorder { return rig.stream(library.FixtureUserId(), track, "bytes=0-1") },
		reached: func() int { return rig.store.streams },
	}
}

func openAudioURLs(t *testing.T, clocks ...func() time.Time) endpointProbe {
	var opts []func(*library.AudioURLHandler)
	for _, clock := range clocks {
		opts = append(opts, library.WithAudioURLLimiterClock(clock))
	}
	rig := newAudioRig(nil, opts)
	track := rig.seedTracks(t, library.FixtureUserId(), 1)[0]
	return endpointProbe{
		hit:     func(int) *httptest.ResponseRecorder { return rig.resolve(library.FixtureUserId(), track) },
		reached: func() int { return rig.store.presigns },
	}
}

func openTrackWrites(t *testing.T, clocks ...func() time.Time) endpointProbe {
	var opts []func(*library.TrackHandler)
	for _, clock := range clocks {
		opts = append(opts, library.WithTrackWriteLimiterClock(clock))
	}
	rig := newWriteRig(opts, nil)
	return endpointProbe{
		hit: func(i int) *httptest.ResponseRecorder {
			return rig.createTrack(t, library.FixtureUserId(), fmt.Sprintf("Track %d", i))
		},
		reached: func() int { return countHeld(t, rig.tracks.CountForUser) },
	}
}

func openPlaylistWrites(t *testing.T, clocks ...func() time.Time) endpointProbe {
	var opts []func(*library.PlaylistHandler)
	for _, clock := range clocks {
		opts = append(opts, library.WithPlaylistWriteLimiterClock(clock))
	}
	rig := newWriteRig(nil, opts)
	return endpointProbe{
		hit: func(i int) *httptest.ResponseRecorder {
			return rig.createPlaylist(t, library.FixtureUserId(), fmt.Sprintf("Playlist %d", i))
		},
		reached: func() int { return countHeld(t, rig.playlists.CountForUser) },
	}
}

func countHeld(t *testing.T, count func(context.Context, sharedids.UserId, int) (int, error)) int {
	t.Helper()
	held, err := count(context.Background(), library.FixtureUserId(), 1<<20)
	if err != nil {
		t.Fatalf("count stored rows: %v", err)
	}
	return held
}

var limitedEndpoints = []limitedEndpoint{
	{"stream", streamBurst, streamEvery, "catalog.audio_rate_limited", "1", openStream},
	{"audio-urls", audioURLBurst, audioURLEvery, "catalog.audio_rate_limited", "1", openAudioURLs},
	{"track writes", trackWriteBurst, trackWriteEvery, "catalog.write_rate_limited", "1", openTrackWrites},
	{"playlist writes", playlistWriteBurst, playlistWriteEvery, "catalog.write_rate_limited", "1", openPlaylistWrites},
}

func assertReached(t *testing.T, probe endpointProbe, want int, why string) {
	t.Helper()
	if got := probe.reached(); got != want {
		t.Fatalf("%s: storage reached %d times, want %d", why, got, want)
	}
}

func assertDefaultBoundary(t *testing.T, ep limitedEndpoint, clock *fakeClock, probe endpointProbe) {
	t.Helper()
	for i := range ep.burst {
		if rec := probe.hit(i); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of the default burst of %d was throttled", i+1, ep.burst)
		}
	}
	assertThrottled(t, probe.hit(ep.burst), ep.code, ep.retryAfter)
	clock.advance(ep.every - time.Millisecond)
	assertThrottled(t, probe.hit(ep.burst+1), ep.code, ep.retryAfter)
	assertReached(t, probe, ep.burst, "a throttled request must never reach storage")
	clock.advance(time.Millisecond)
	if rec := probe.hit(ep.burst + 2); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("one default interval of %s must refill a token, still throttled", ep.every)
	}
	assertReached(t, probe, ep.burst+1, "a refilled token must reach storage")
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
