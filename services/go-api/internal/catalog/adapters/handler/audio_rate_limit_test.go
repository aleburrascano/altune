package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
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

type audioFakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newAudioFakeClock() *audioFakeClock { return &audioFakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *audioFakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *audioFakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// countingAudioStore counts GetObject-equivalent Stream calls and presigns so
// a test can prove a throttled request never reached object storage.
type countingAudioStore struct {
	*catalogtest.AudioStore
	mu       sync.Mutex
	streams  int
	presigns int
}

func (s *countingAudioStore) Stream(ctx context.Context, ref string) (ports.AudioStream, int64, error) {
	s.mu.Lock()
	s.streams++
	s.mu.Unlock()
	return s.AudioStore.Stream(ctx, ref)
}

func (s *countingAudioStore) PresignGet(_ context.Context, ref string, _ time.Duration) (string, error) {
	s.mu.Lock()
	s.presigns++
	s.mu.Unlock()
	return "https://storage.test/" + ref + "?sig=x", nil
}

// verifyBearerAsUser treats the bearer token as the caller's user id, so one
// router can serve several principals.
var verifyBearerAsUser = auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
	id, err := uuid.Parse(token)
	if err != nil {
		return auth.VerifiedToken{}, err
	}
	return auth.VerifiedToken{UserID: shared.NewUserId(id), ExpiresAt: time.Now().Add(time.Hour)}, nil
})

const audioTestBytes = 64 * 1024

// audioRig is the real stream and audio-url route table behind the real auth
// middleware, over fakes that count every backend call.
type audioRig struct {
	router chi.Router
	store  *countingAudioStore
	repo   *catalogtest.TrackRepo
}

func newAudioRig(streamOpts []func(*StreamHandler), urlOpts []func(*AudioURLHandler)) *audioRig {
	repo := catalogtest.NewTrackRepo()
	store := &countingAudioStore{AudioStore: catalogtest.NewAudioStore()}
	streamSvc := service.NewStreamTrackService(repo, store)
	urlSvc := service.NewAudioURLService(repo, store)

	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyBearerAsUser))
	NewStreamHandler(streamSvc, streamOpts...).Routes(r)
	NewAudioURLHandler(urlSvc, urlOpts...).Routes(r)
	return &audioRig{router: r, store: store, repo: repo}
}

// seedTracks gives user n ready tracks with real audio bytes.
func (rig *audioRig) seedTracks(user shared.UserId, n int) []*domain.Track {
	tracks := make([]*domain.Track, 0, n)
	for i := range n {
		ref := fmt.Sprintf("audio/%s-%d.opus", user.String(), i)
		track := makeReadyTrack(user, fmt.Sprintf("Track %d", i), "Artist", "Album", ref)
		rig.repo.Seed(track)
		rig.store.Seed(ref, bytes.Repeat([]byte{0x5a}, audioTestBytes))
		tracks = append(tracks, track)
	}
	return tracks
}

func (rig *audioRig) do(req *http.Request, user shared.UserId) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer "+user.String())
	rec := httptest.NewRecorder()
	rig.router.ServeHTTP(rec, req)
	return rec
}

// stream issues GET /tracks/{id}/audio, with a Range header when rangeHdr is set.
func (rig *audioRig) stream(user shared.UserId, track *domain.Track, rangeHdr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	return rig.do(req, user)
}

func (rig *audioRig) resolve(user shared.UserId, tracks ...*domain.Track) *httptest.ResponseRecorder {
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, `"`+t.ID.UUID().String()+`"`)
	}
	body := `{"track_ids":[` + strings.Join(ids, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/audio-urls", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return rig.do(req, user)
}

func assertAudioThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantRetryAfter string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"catalog.audio_rate_limited"`) {
		t.Fatalf("throttled body must carry the rate-limit code, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != wantRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, wantRetryAfter)
	}
}

func TestStreamRateLimit_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: 2 * time.Second, Burst: 5}
	rig := newAudioRig([]func(*StreamHandler){WithStreamRateLimit(limit), withStreamClock(clock.now)}, nil)
	user := shared.NewUserId(uuid.New())
	track := rig.seedTracks(user, 1)[0]

	for i := range limit.Burst {
		if rec := rig.stream(user, track, ""); rec.Code != http.StatusOK {
			t.Fatalf("request %d inside the burst must stream, got %d", i, rec.Code)
		}
	}
	for range 50 {
		assertAudioThrottled(t, rig.stream(user, track, "bytes=0-1"), "2")
	}
	if rig.store.streams != limit.Burst {
		t.Fatalf("throttled requests must not reach object storage: %d streams, want %d", rig.store.streams, limit.Burst)
	}

	clock.advance(limit.Every)
	if rec := rig.stream(user, track, "bytes=0-1"); rec.Code != http.StatusPartialContent {
		t.Fatalf("a refilled token must admit the next range request, got %d", rec.Code)
	}
}

func TestAudioURLRateLimit_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: time.Second, Burst: 5}
	rig := newAudioRig(nil, []func(*AudioURLHandler){WithAudioURLRateLimit(limit), withAudioURLClock(clock.now)})
	user := shared.NewUserId(uuid.New())
	track := rig.seedTracks(user, 1)[0]

	for i := range limit.Burst {
		if rec := rig.resolve(user, track); rec.Code != http.StatusOK {
			t.Fatalf("request %d inside the burst must resolve, got %d", i, rec.Code)
		}
	}
	for range 50 {
		assertAudioThrottled(t, rig.resolve(user, track), "1")
	}
	if rig.store.presigns != limit.Burst {
		t.Fatalf("throttled requests must not presign: %d presigns, want %d", rig.store.presigns, limit.Burst)
	}

	clock.advance(limit.Every)
	if rec := rig.resolve(user, track); rec.Code != http.StatusOK {
		t.Fatalf("a refilled token must admit the next resolve, got %d", rec.Code)
	}
}

func TestAudioRateLimit_IsPerPrincipalAndPerEndpoint(t *testing.T) {
	clock := newAudioFakeClock()
	one := AudioRateLimit{Every: time.Hour, Burst: 1}
	rig := newAudioRig(
		[]func(*StreamHandler){WithStreamRateLimit(one), withStreamClock(clock.now)},
		[]func(*AudioURLHandler){WithAudioURLRateLimit(one), withAudioURLClock(clock.now)},
	)
	noisy, quiet := shared.NewUserId(uuid.New()), shared.NewUserId(uuid.New())
	noisyTrack := rig.seedTracks(noisy, 1)[0]
	quietTrack := rig.seedTracks(quiet, 1)[0]

	rig.stream(noisy, noisyTrack, "")
	assertAudioThrottled(t, rig.stream(noisy, noisyTrack, ""), "3600")

	if rec := rig.resolve(noisy, noisyTrack); rec.Code != http.StatusOK {
		t.Fatalf("a spent stream budget must not throttle audio-urls, got %d", rec.Code)
	}
	if rec := rig.stream(quiet, quietTrack, ""); rec.Code != http.StatusOK {
		t.Fatalf("another user's stream budget must be untouched, got %d", rec.Code)
	}
}

func TestAudioRateLimit_UnauthenticatedStillGets401(t *testing.T) {
	rig := newAudioRig(nil, nil)

	stream := httptest.NewRecorder()
	rig.router.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/tracks/"+uuid.NewString()+"/audio", nil))
	resolve := httptest.NewRecorder()
	rig.router.ServeHTTP(resolve, httptest.NewRequest(http.MethodPost, "/audio-urls", strings.NewReader(`{"track_ids":[]}`)))

	if stream.Code != http.StatusUnauthorized || resolve.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal must still be a 401, got stream %d audio-urls %d", stream.Code, resolve.Code)
	}
}

// TestAudioRateLimit_DefaultsAdmitRealClientTraffic replays the mobile
// client's heaviest legitimate patterns against the default limits on one
// account; not one request may be throttled.
//
//   - Offline pin: the pinned-download worker resolves one track per
//     /audio-urls call, back to back. A 200-track batch is replayed with zero
//     time between calls (every download failing instantly), while playback
//     starts alongside it (a 25-track presign window plus a prefetch).
//   - Listening: two hours on the streaming fallback (presign unavailable).
//     A 20-track skip storm in 10 seconds, each skip an AVPlayer bytes=0-1
//     probe plus two Range reads, then 3-minute tracks each loaded with a probe,
//     six buffer-refill Range reads and ten seeks, with a prefetch resolve per
//     track change and a presign-window slide every 20 tracks.
func TestAudioRateLimit_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newAudioFakeClock()
	rig := newAudioRig(
		[]func(*StreamHandler){withStreamClock(clock.now)},
		[]func(*AudioURLHandler){withAudioURLClock(clock.now)},
	)
	user := shared.NewUserId(uuid.New())
	tracks := rig.seedTracks(user, 200)

	mustResolve := func(what string, batch ...*domain.Track) {
		t.Helper()
		if rec := rig.resolve(user, batch...); rec.Code != http.StatusOK {
			t.Fatalf("%s throttled: %d %q", what, rec.Code, rec.Body.String())
		}
	}
	mustStream := func(what string, track *domain.Track, rangeHdr string) {
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

func TestAudioRateLimiter_EvictsRefilledBuckets(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: time.Second, Burst: 2}
	l := newAudioRateLimiter(limit, clock.now)

	for range 1000 {
		l.allow(uuid.NewString())
	}
	clock.advance(limit.Every * time.Duration(limit.Burst))
	l.allow("fresh")

	if got := len(l.buckets); got != 1 {
		t.Fatalf("idle refilled buckets must be evicted, %d remain", got)
	}
}

// TestWriteRateLimits_DefaultsAdmitRealClientTraffic replays the client's
// heaviest legitimate write patterns against the default budgets on one
// account, with no time passing between calls; not one request may be
// throttled.
//
//   - "Save all" on a 100-track compilation: one POST /tracks per unowned
//     track, four in flight, then the same again on a second album.
//   - Adding a track to every playlist it owns from the add-to-playlist sheet:
//     one batch call per playlist.
func TestWriteRateLimits_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newAudioFakeClock()
	rig := newWriteRig(
		[]func(*TrackHandler){withTrackWriteClock(clock.now)},
		[]func(*PlaylistHandler){withPlaylistWriteClock(clock.now)},
	)
	user := shared.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, user)

	for i := range 150 {
		rec := rig.createTrack(user, fmt.Sprintf("Compilation Track %d", i))
		if rec.Code != http.StatusCreated {
			t.Fatalf("save-all track %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}

	for i := range 40 {
		if rec := rig.addTracksToPlaylist(user, playlistId, trackId); rec.Code != http.StatusOK {
			t.Fatalf("add-to-playlist %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
}
