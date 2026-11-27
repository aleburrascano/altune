package handler_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	sharedids "altune/go-api/internal/shared/ids"
	"bytes"
	"context"
	"errors"
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

func (s *countingAudioStore) SignsLinks() bool { return true }

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

func TestHandlers_RefuseANilClock(t *testing.T) {
	constructors := map[string]func() error{
		"NewStreamHandler": func() error {
			_, err := library.NewStreamHandler(nil, nil)
			return err
		},
		"NewAudioURLHandler": func() error {
			_, err := library.NewAudioURLHandler(nil, nil)
			return err
		},
		"NewPlaylistHandler": func() error {
			_, err := library.NewPlaylistHandler(nil, nil, nil)
			return err
		},
		"NewTrackHandler": func() error {
			_, err := library.NewTrackHandler(nil, nil, nil, nil, nil, nil, nil)
			return err
		},
	}
	for name, construct := range constructors {
		t.Run(name, func(t *testing.T) {
			err := construct()
			if !errors.Is(err, library.ErrHandlerClockRequired) || !strings.Contains(err.Error(), name) {
				t.Fatalf("constructing with a nil clock = %v, want ErrHandlerClockRequired naming %s", err, name)
			}
		})
	}
}

func firstClock(clocks []func() time.Time) func() time.Time {
	if len(clocks) == 0 {
		return time.Now
	}
	return clocks[0]
}

func newAudioRig(now func() time.Time) *audioRig {
	repo := library.NewInMemoryTrackRepo()
	store := &countingAudioStore{InMemoryAudioStore: library.NewInMemoryAudioStore()}
	r := chi.NewRouter()
	r.Use(asBearer)
	library.MustNewStreamHandler(library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), &librarytest.Metrics{}, library.AlwaysEnabled, clock.Now), now).Routes(r)
	library.MustNewAudioURLHandler(library.MustNewAudioURLService(repo, store, now, &librarytest.Metrics{}), now).Routes(r)
	return &audioRig{router: r, store: store, repo: repo}
}

func (rig *audioRig) seedTracks(t *testing.T, user sharedids.UserId, n int) []*library.Track {
	t.Helper()
	tracks := make([]*library.Track, 0, n)
	for i := range n {
		ref := fmt.Sprintf("audio/%s-%d.opus", user.String(), i)
		tracks = append(tracks, librarytest.SeedReadyTrack(t, rig.repo, user, fmt.Sprintf("Track %d", i), "Artist", "Album", ref))
		rig.store.Seed(ref, bytes.Repeat([]byte{0x5a}, audioTestBytes))
	}
	return tracks
}

func (rig *audioRig) stream(user sharedids.UserId, track *library.Track, rangeHdr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID().UUID().String()+"/audio", nil)
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	return sendAs(rig.router, req, user)
}

func (rig *audioRig) resolve(user sharedids.UserId, tracks ...*library.Track) *httptest.ResponseRecorder {
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, `"`+t.ID().UUID().String()+`"`)
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
	rig := newAudioRig(clock.now)
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

func TestAudioRateLimit_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newFakeClock()
	rig := newAudioRig(clock.now)
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
	rig := newWriteRig(clock.now)
	user := sharedids.NewUserId(uuid.New())
	playlist := librarytest.SeedPlaylist(t, rig.playlists, user, "Seeded")
	track := librarytest.SeedTrack(t, rig.tracks, user, "Seeded Track", "Artist", "Album")

	for i := range 150 {
		if rec := rig.createTrack(t, user, fmt.Sprintf("Compilation Track %d", i)); rec.Code != http.StatusCreated {
			t.Fatalf("save-all track %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	batch := map[string][]string{"track_ids": {track.ID().String()}}
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
	rig := newAudioRig(firstClock(clocks))
	track := rig.seedTracks(t, librarytest.UserId(), 1)[0]
	return endpointProbe{
		hit:     func(int) *httptest.ResponseRecorder { return rig.stream(librarytest.UserId(), track, "bytes=0-1") },
		reached: func() int { return rig.store.streams },
	}
}

func openAudioURLs(t *testing.T, clocks ...func() time.Time) endpointProbe {
	rig := newAudioRig(firstClock(clocks))
	track := rig.seedTracks(t, librarytest.UserId(), 1)[0]
	return endpointProbe{
		hit:     func(int) *httptest.ResponseRecorder { return rig.resolve(librarytest.UserId(), track) },
		reached: func() int { return rig.store.presigns },
	}
}

func openTrackWrites(t *testing.T, clocks ...func() time.Time) endpointProbe {
	rig := newWriteRig(firstClock(clocks))
	return endpointProbe{
		hit: func(i int) *httptest.ResponseRecorder {
			return rig.createTrack(t, librarytest.UserId(), fmt.Sprintf("Track %d", i))
		},
		reached: func() int { return countHeld(t, rig.tracks.CountForUser) },
	}
}

func openPlaylistWrites(t *testing.T, clocks ...func() time.Time) endpointProbe {
	rig := newWriteRig(firstClock(clocks))
	return endpointProbe{
		hit: func(i int) *httptest.ResponseRecorder {
			return rig.createPlaylist(t, librarytest.UserId(), fmt.Sprintf("Playlist %d", i))
		},
		reached: func() int { return countHeld(t, rig.playlists.CountForUser) },
	}
}

func countHeld(t *testing.T, count func(context.Context, sharedids.UserId, int) (int, error)) int {
	t.Helper()
	held, err := count(context.Background(), librarytest.UserId(), 1<<20)
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
