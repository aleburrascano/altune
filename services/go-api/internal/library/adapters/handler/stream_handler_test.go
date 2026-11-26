package handler_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/slowclient"
	"altune/go-api/internal/shared/testpoll"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func streamOnlyRoute(repo *library.InMemoryTrackRepo, store library.AudioStore, scheduler library.AcquisitionScheduler) chi.Router {
	h := library.MustNewStreamHandler(library.NewStreamTrackService(repo, store, scheduler, library.NoopAudioStoreMetrics(), library.AlwaysEnabled), time.Now)
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Get("/tracks/{trackId}/stream", h.HandleStreamAudio)
	return router
}

func streamRoutes(store library.AudioStore) (*library.InMemoryTrackRepo, chi.Router) {
	return streamRoutesAt(store, time.Now)
}

func streamRoutesAt(store library.AudioStore, now func() time.Time) (*library.InMemoryTrackRepo, chi.Router) {
	repo := library.NewInMemoryTrackRepo()
	router := chi.NewRouter()
	router.Use(asBearer)
	library.MustNewStreamHandler(library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), library.NoopAudioStoreMetrics(), library.AlwaysEnabled), now).Routes(router)
	return repo, router
}

func seedReady(t *testing.T, repo *library.InMemoryTrackRepo, title, audioRef string) *library.Track {
	t.Helper()
	return library.SeedReadyTrack(t, repo, library.FixtureUserId(), title, "Artist", "Album", audioRef)
}

func TestHandleStreamAudio(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*testing.T, *library.InMemoryTrackRepo, *library.InMemoryAudioStore) string
		wantStatus      int
		wantContentType string
		wantBodyLen     int
	}{
		{
			name: "mp3 track serves audio/mpeg",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				track := seedReady(t, repo, "Track", "audio/123.mp3")
				store.Seed("audio/123.mp3", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/mpeg",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "m4a track serves audio/mp4",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				track := seedReady(t, repo, "Track", "audio/456.m4a")
				store.Seed("audio/456.m4a", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/mp4",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "opus track serves audio/opus",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				track := seedReady(t, repo, "Track", "audio/789.opus")
				store.Seed("audio/789.opus", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/opus",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "invalid track ID returns 400",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "track not found returns 404",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not streamable) returns 404",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				track := library.SeedTrack(t, repo, library.FixtureUserId(), "Pending", "Artist", "Album")
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "ready track with missing audio file returns 404 and reconciles",
			setup: func(t *testing.T, repo *library.InMemoryTrackRepo, store *library.InMemoryAudioStore) string {
				track := seedReady(t, repo, "Gone", "audio/gone.opus")
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			store := library.NewInMemoryAudioStore()
			trackId := tt.setup(t, repo, store)
			router := streamOnlyRoute(repo, store, library.NoopAcquisitionScheduler())

			rec := request(t, router, http.MethodGet, "/tracks/"+trackId+"/stream", nil)

			assertStatus(t, rec, tt.wantStatus)
			if tt.wantContentType != "" {
				if ct := rec.Header().Get("Content-Type"); ct != tt.wantContentType {
					t.Errorf("Content-Type = %q, want %q", ct, tt.wantContentType)
				}
			}
			if tt.wantBodyLen > 0 && rec.Body.Len() != tt.wantBodyLen {
				t.Errorf("body length = %d, want %d", rec.Body.Len(), tt.wantBodyLen)
			}
		})
	}
}

func TestHandleStreamAudio_MissingAudio_SchedulesReacquisition(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	sched := &library.InMemoryScheduler{}
	track := seedReady(t, repo, "Gone", "audio/gone.opus")
	router := streamOnlyRoute(repo, library.NewInMemoryAudioStore(), sched)

	rec := request(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/stream", nil)

	assertStatus(t, rec, http.StatusNotFound)
	if len(sched.TrackIds) != 1 {
		t.Fatalf("expected 1 scheduled reacquisition, got %d", len(sched.TrackIds))
	}
	if sched.TrackIds[0] != track.ID {
		t.Errorf("scheduled track = %v, want %v", sched.TrackIds[0], track.ID)
	}
}

func TestHandleStreamAudio_NoAuth(t *testing.T) {
	router := streamOnlyRoute(library.NewInMemoryTrackRepo(), library.NewInMemoryAudioStore(), library.NoopAcquisitionScheduler())

	rec := sendAnonymous(router, httptest.NewRequest(http.MethodGet, "/tracks/"+uuid.New().String()+"/stream", nil))

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleRecover(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	sched := &library.InMemoryScheduler{}
	foreign := library.SeedReadyTrack(t, repo, library.FixtureOtherUserId(), "Theirs", "Artist", "Album", "audio/theirs.opus")
	pending := library.SeedTrack(t, repo, library.FixtureUserId(), "Pending", "Artist", "Album")
	gone := seedReady(t, repo, "Gone", "audio/gone.opus")
	router := chi.NewRouter()
	router.Use(asBearer)
	svc := library.NewStreamTrackService(repo, library.NewInMemoryAudioStore(), sched, library.NoopAudioStoreMetrics(), library.AlwaysEnabled)
	library.MustNewStreamHandler(svc, time.Now).Routes(router)

	tests := []struct {
		name       string
		trackId    string
		wantStatus int
	}{
		{"nonexistent track returns 404", uuid.New().String(), http.StatusNotFound},
		{"foreign track returns 404", foreign.ID.UUID().String(), http.StatusNotFound},
		{"owned non-streamable track is an accepted no-op", pending.ID.UUID().String(), http.StatusAccepted},
		{"owned track with missing audio is accepted", gone.ID.UUID().String(), http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := request(t, router, http.MethodPost, "/tracks/"+tt.trackId+"/audio/recover", nil)
			assertStatus(t, rec, tt.wantStatus)
		})
	}
	if len(sched.TrackIds) != 1 || sched.TrackIds[0] != gone.ID {
		t.Fatalf("scheduled = %v, want only the owned missing-audio track %v", sched.TrackIds, gone.ID)
	}
}

type probeCountingStore struct {
	*library.InMemoryAudioStore
	probes int
}

func (s *probeCountingStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	s.probes++
	return s.InMemoryAudioStore.Exists(ctx, audioRef)
}

func TestHandleRecover_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newFakeClock()
	store := &probeCountingStore{InMemoryAudioStore: library.NewInMemoryAudioStore()}
	repo, router := streamRoutesAt(store, clock.now)
	track := seedReady(t, repo, "Track", "audio/present.opus")
	store.Seed("audio/present.opus", []byte("fake-audio-data"))
	path := "/tracks/" + track.ID.UUID().String() + "/audio/recover"

	for i := range streamBurst {
		if rec := request(t, router, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
			t.Fatalf("recover %d inside the burst must be accepted, got %d", i, rec.Code)
		}
	}
	assertAudioThrottled(t, request(t, router, http.MethodPost, path, nil), "1")

	if store.probes != streamBurst {
		t.Fatalf("a throttled recover must not probe storage: %d HEADs, want %d", store.probes, streamBurst)
	}
	clock.advance(streamEvery)
	if rec := request(t, router, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("a refilled token must admit the next recover, got %d", rec.Code)
	}
}

var errStorageReadFailed = errors.New("storage read failed")

type truncatingAudioStore struct {
	*library.InMemoryAudioStore
	failAfter int64
}

func (s *truncatingAudioStore) Stream(ctx context.Context, audioRef string) (library.AudioStream, int64, error) {
	stream, size, err := s.InMemoryAudioStore.Stream(ctx, audioRef)
	if err != nil {
		return nil, 0, err
	}
	return &truncatingStream{AudioStream: stream, failAfter: s.failAfter}, size, nil
}

type truncatingStream struct {
	library.AudioStream
	failAfter int64
	read      int64
}

func (s *truncatingStream) Read(p []byte) (int, error) {
	remaining := s.failAfter - s.read
	if remaining <= 0 {
		return 0, errStorageReadFailed
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := s.AudioStream.Read(p)
	s.read += int64(n)
	return n, err
}

func findLogRecord(t *testing.T, ring *logging.RingBuffer, message string) logging.CapturedRecord {
	t.Helper()
	for _, rec := range ring.Snapshot() {
		if rec.Message == message {
			return rec
		}
	}
	t.Fatalf("no %q log line was written", message)
	return logging.CapturedRecord{}
}

func TestHandleStreamAudio_MidBodyReadErrorIsLoggedAsTruncated(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info")

	const deliverable = 4096
	store := &truncatingAudioStore{InMemoryAudioStore: library.NewInMemoryAudioStore(), failAfter: deliverable}
	repo, router := streamRoutes(store)
	track := seedReady(t, repo, "Half", "audio/half.opus")
	store.Seed("audio/half.opus", bytes.Repeat([]byte{0x5a}, 4*deliverable))

	rec := request(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)

	if rec.Body.Len() != deliverable {
		t.Fatalf("body = %d bytes, want the %d delivered before the read failed", rec.Body.Len(), deliverable)
	}
	served := findLogRecord(t, ring, "stream.served")
	if served.Attrs["outcome"] != "error" {
		t.Errorf("outcome = %q, want error: a truncated stream must not log as a success", served.Attrs["outcome"])
	}
	if got, want := served.Attrs["bytes"], strconv.Itoa(deliverable); got != want {
		t.Errorf("bytes = %q, want %q", got, want)
	}
	if served.Level != slog.LevelWarn.String() {
		t.Errorf("level = %q, want %q: a server-side read error is not routine", served.Level, slog.LevelWarn)
	}
}

func TestHandleStreamAudio_WholeBodyIsLoggedAsServed(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info")

	audio := []byte("fake-audio-data")
	store := library.NewInMemoryAudioStore()
	repo, router := streamRoutes(store)
	track := seedReady(t, repo, "Whole", "audio/whole.opus")
	store.Seed("audio/whole.opus", audio)

	request(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)

	served := findLogRecord(t, ring, "stream.served")
	if served.Attrs["outcome"] != "ok" {
		t.Errorf("outcome = %q, want ok", served.Attrs["outcome"])
	}
	if got, want := served.Attrs["bytes"], strconv.Itoa(len(audio)); got != want {
		t.Errorf("bytes = %q, want the whole body %q", got, want)
	}
	if served.Level != slog.LevelInfo.String() {
		t.Errorf("level = %q, want %q", served.Level, slog.LevelInfo)
	}
}

func TestHandleStreamAudio_ClientAbortIsNotAServerError(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info")

	store := &truncatingAudioStore{InMemoryAudioStore: library.NewInMemoryAudioStore(), failAfter: 1024}
	repo, router := streamRoutes(store)
	track := seedReady(t, repo, "Skipped", "audio/skipped.opus")
	store.Seed("audio/skipped.opus", bytes.Repeat([]byte{0x5a}, 4096))

	ctx, abort := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil).WithContext(ctx)
	abort()
	sendAs(router, req, library.FixtureUserId())

	served := findLogRecord(t, ring, "stream.served")
	if served.Attrs["outcome"] != "aborted" {
		t.Errorf("outcome = %q, want aborted", served.Attrs["outcome"])
	}
	if served.Level != slog.LevelInfo.String() {
		t.Errorf("level = %q, want %q: a client hanging up is not this server failing", served.Level, slog.LevelInfo)
	}
}

func TestHandleStreamAudio_FramesPrivateAudio(t *testing.T) {
	store := library.NewInMemoryAudioStore()
	repo, router := streamRoutes(store)
	track := seedReady(t, repo, "Track", "audio/framed.opus")
	store.Seed("audio/framed.opus", bytes.Repeat([]byte{0x5a}, 4096))
	path := "/tracks/" + track.ID.UUID().String() + "/audio"

	whole := request(t, router, http.MethodGet, path, nil)
	rangeReq := httptest.NewRequest(http.MethodGet, path, nil)
	rangeReq.Header.Set("Range", "bytes=1024-2047")
	ranged := sendAs(router, rangeReq, library.FixtureUserId())

	assertStatus(t, whole, http.StatusOK)
	assertPrivateAudioHeaders(t, whole, "private")
	if ranged.Code != http.StatusPartialContent {
		t.Fatalf("range request status = %d, want 206: the caching headers must not block seeking", ranged.Code)
	}
	assertPrivateAudioHeaders(t, ranged, "private")
	if ranged.Body.Len() != 1024 {
		t.Errorf("range body = %d bytes, want 1024", ranged.Body.Len())
	}
}

const (
	audioRouteDeadline = time.Millisecond
	audioSize          = 2 << 20
)

func serveAudioBehindRouteDeadline(t *testing.T, routeDeadline time.Duration, opts ...func(*library.StreamHandler)) audioServer {
	t.Helper()
	repo := library.NewInMemoryTrackRepo()
	store := library.NewInMemoryAudioStore()
	track := seedReady(t, repo, "Long", "audio/long.opus")
	store.Seed("audio/long.opus", bytes.Repeat([]byte{0xAB}, audioSize))

	r := chi.NewRouter()
	var finished atomic.Int32
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer finished.Add(1)
			next.ServeHTTP(w, req)
		})
	})
	r.Use(httpserver.WriteDeadline(routeDeadline))
	r.Use(httpserver.RequestLogger)
	r.Use(asBearer)
	library.MustNewStreamHandler(library.NewStreamTrackService(repo, store, library.NoopAcquisitionScheduler(), library.NoopAudioStoreMetrics(), library.AlwaysEnabled), time.Now, opts...).Routes(r)
	srv := slowclient.NewServer(t, r)

	path := "/tracks/" + track.ID.UUID().String() + "/audio"
	auth := http.Header{"Authorization": {"Bearer " + library.FixtureUserId().String()}}
	get := func() *http.Response {
		conn, br := slowclient.Get(t, srv, path, auth)
		resp := slowclient.ReadResponse(t, conn, br)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		return resp
	}
	return audioServer{get: get, handled: func() bool { return finished.Load() > 0 }}
}

type audioServer struct {
	get     func() *http.Response
	handled func() bool
}

func TestHandleStreamAudio_SlowNetworkIsNotCutOffByRouteDeadline(t *testing.T) {
	srv := serveAudioBehindRouteDeadline(t, audioRouteDeadline, library.WithStreamWriteIdleTimeout(time.Second))
	resp := srv.get()
	defer func() { _ = resp.Body.Close() }()

	began := time.Now()
	got := 0
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		got += n
		if err != nil {
			break
		}
	}
	elapsed := time.Since(began)
	if got != audioSize {
		t.Fatalf("received %d of %d bytes after %s: slow download was cut off", got, audioSize, elapsed)
	}
	if elapsed <= 3*audioRouteDeadline {
		t.Fatalf("download took %s, not well past the %s route deadline, so it proves nothing", elapsed, audioRouteDeadline)
	}
}

func TestHandleStreamAudio_StalledClientIsCutOff(t *testing.T) {
	const idle = 150 * time.Millisecond
	srv := serveAudioBehindRouteDeadline(t, audioRouteDeadline, library.WithStreamWriteIdleTimeout(idle))
	resp := srv.get()
	defer func() { _ = resp.Body.Close() }()

	testpoll.Until(t, 10*time.Second, "the server to give up on the client that stopped reading", srv.handled)
	body, err := io.ReadAll(resp.Body)
	if err == nil && len(body) == audioSize {
		t.Fatalf("stalled client still received the whole %d-byte body: no idle write deadline", audioSize)
	}
}

func TestHandleStreamAudio_ANonPositiveIdleTimeoutKeepsTheDefault(t *testing.T) {
	for _, idle := range []time.Duration{0, -time.Second} {
		t.Run(idle.String(), func(t *testing.T) {
			srv := serveAudioBehindRouteDeadline(t, audioRouteDeadline, library.WithStreamWriteIdleTimeout(idle))
			resp := srv.get()
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			if err != nil || len(body) != audioSize {
				t.Fatalf("received %d of %d bytes (err %v): the idle timeout was taken instead of ignored", len(body), audioSize, err)
			}
		})
	}
}
