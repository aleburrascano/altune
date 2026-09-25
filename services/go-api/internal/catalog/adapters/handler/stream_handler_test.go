package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/httputil/httputiltest"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestHandleStreamAudio(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*catalogtest.TrackRepo, *catalogtest.AudioStore) string
		wantStatus      int
		wantContentType string
		wantBodyLen     int
	}{
		{
			name: "mp3 track serves audio/mpeg",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/123.mp3")
				repo.Seed(track)
				store.Seed("audio/123.mp3", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/mpeg",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "m4a track serves audio/mp4",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/456.m4a")
				repo.Seed(track)
				store.Seed("audio/456.m4a", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/mp4",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "opus track serves audio/opus",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/789.opus")
				repo.Seed(track)
				store.Seed("audio/789.opus", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/opus",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "track not found returns 404",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not streamable) returns 404",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				track := makeTrack(testUserId, "Pending", "Artist", "Album")
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "ready track with missing audio file returns 404 and reconciles",
			setup: func(repo *catalogtest.TrackRepo, store *catalogtest.AudioStore) string {
				track := makeReadyTrack(testUserId, "Gone", "Artist", "Album", "audio/gone.opus")
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			store := catalogtest.NewAudioStore()
			trackId := tt.setup(repo, store)
			_, router := buildStreamHandler(repo, store, nil)

			rec := serve(t, router, http.MethodGet, "/tracks/"+trackId+"/stream", nil)

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantContentType != "" {
				ct := rec.Header().Get("Content-Type")
				if ct != tt.wantContentType {
					t.Errorf("Content-Type = %q, want %q", ct, tt.wantContentType)
				}
			}
			if tt.wantBodyLen > 0 {
				if rec.Body.Len() != tt.wantBodyLen {
					t.Errorf("body length = %d, want %d", rec.Body.Len(), tt.wantBodyLen)
				}
			}
		})
	}
}

func TestHandleStreamAudio_MissingAudio_SchedulesReacquisition(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	sched := &catalogtest.Scheduler{}
	track := makeReadyTrack(testUserId, "Gone", "Artist", "Album", "audio/gone.opus")
	repo.Seed(track)

	_, router := buildStreamHandler(repo, store, sched)
	rec := serve(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/stream", nil)

	assertStatus(t, rec, http.StatusNotFound)

	if len(sched.TrackIds) != 1 {
		t.Fatalf("expected 1 scheduled reacquisition, got %d", len(sched.TrackIds))
	}
	if sched.TrackIds[0] != track.ID {
		t.Errorf("scheduled track = %v, want %v", sched.TrackIds[0], track.ID)
	}
}

func TestHandleStreamAudio_NoAuth(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	_, router := buildStreamHandler(repo, store, nil)

	rec := serveNoAuth(t, router, http.MethodGet, "/tracks/"+uuid.New().String()+"/stream", nil)

	assertStatus(t, rec, http.StatusUnauthorized)
}

// TestHandleRecover pins #1049 at the HTTP edge: recovery of a nonexistent or
// foreign track answers 404, while an owned track, even a non-streamable one
// where recovery is a no-op, still answers 202.
func TestHandleRecover(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{}
	foreign := makeReadyTrack(shared.NewUserId(uuid.New()), "Theirs", "Artist", "Album", "audio/theirs.opus")
	pending := makeTrack(testUserId, "Pending", "Artist", "Album")
	gone := makeReadyTrack(testUserId, "Gone", "Artist", "Album", "audio/gone.opus")
	repo.Seed(foreign)
	repo.Seed(pending)
	repo.Seed(gone)

	h, _ := buildStreamHandler(repo, catalogtest.NewAudioStore(), sched)
	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	h.Routes(router)

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
			rec := serve(t, router, http.MethodPost, "/tracks/"+tt.trackId+"/audio/recover", nil)
			assertStatus(t, rec, tt.wantStatus)
		})
	}
	if len(sched.TrackIds) != 1 || sched.TrackIds[0] != gone.ID {
		t.Fatalf("scheduled = %v, want only the owned missing-audio track %v", sched.TrackIds, gone.ID)
	}
}

// mountStreamRoutes serves the real stream route table (audio GET plus
// recover, both behind their throttle) over store, authenticated as testUserId.
func mountStreamRoutes(store ports.AudioStore, opts ...func(*StreamHandler)) (*catalogtest.TrackRepo, chi.Router) {
	repo := catalogtest.NewTrackRepo()
	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	NewStreamHandler(service.NewStreamTrackService(repo, store), opts...).Routes(router)
	return repo, router
}

// probeCountingStore counts the storage HEAD that recover performs, so a test
// can prove a throttled recover never reached object storage.
type probeCountingStore struct {
	*catalogtest.AudioStore
	probes int
}

func (s *probeCountingStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	s.probes++
	return s.AudioStore.Exists(ctx, audioRef)
}

// TestHandleRecover_FloodFromOnePrincipalIsThrottled pins #2199: recover reads
// the row and HEADs storage on every call and answers 202 whether or not the
// file is there, so off the audio budget it is a free storage probe.
func TestHandleRecover_FloodFromOnePrincipalIsThrottled(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: 2 * time.Second, Burst: 3}
	store := &probeCountingStore{AudioStore: catalogtest.NewAudioStore()}
	repo, router := mountStreamRoutes(store, WithStreamRateLimit(limit), withStreamClock(clock.now))
	track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/present.opus")
	repo.Seed(track)
	store.Seed("audio/present.opus", []byte("fake-audio-data"))
	path := "/tracks/" + track.ID.UUID().String() + "/audio/recover"

	for i := range limit.Burst {
		if rec := serve(t, router, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
			t.Fatalf("recover %d inside the burst must be accepted, got %d", i, rec.Code)
		}
	}
	assertAudioThrottled(t, serve(t, router, http.MethodPost, path, nil), "2")

	if store.probes != limit.Burst {
		t.Fatalf("a throttled recover must not probe storage: %d HEADs, want %d", store.probes, limit.Burst)
	}
	clock.advance(limit.Every)
	if rec := serve(t, router, http.MethodPost, path, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("a refilled token must admit the next recover, got %d", rec.Code)
	}
}

var errStorageReadFailed = errors.New("storage read failed")

// truncatingAudioStore hands back a stream that fails partway through the
// body: the mid-transfer read error an object store produces when a connection
// dies after the response headers are already out.
type truncatingAudioStore struct {
	*catalogtest.AudioStore
	failAfter int64
}

func (s *truncatingAudioStore) Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error) {
	stream, size, err := s.AudioStore.Stream(ctx, audioRef)
	if err != nil {
		return nil, 0, err
	}
	return &truncatingStream{AudioStream: stream, failAfter: s.failAfter}, size, nil
}

type truncatingStream struct {
	ports.AudioStream
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

// TestHandleStreamAudio_MidBodyReadErrorIsLoggedAsTruncated pins #2199:
// http.ServeContent reports neither an error nor a byte count, so a stream
// that died mid-body used to log exactly like a whole one.
func TestHandleStreamAudio_MidBodyReadErrorIsLoggedAsTruncated(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	const deliverable = 4096
	store := &truncatingAudioStore{AudioStore: catalogtest.NewAudioStore(), failAfter: deliverable}
	repo, router := mountStreamRoutes(store)
	track := makeReadyTrack(testUserId, "Half", "Artist", "Album", "audio/half.opus")
	repo.Seed(track)
	store.Seed("audio/half.opus", bytes.Repeat([]byte{0x5a}, 4*deliverable))

	rec := serve(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)

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
	ring := logging.Setup("info", false)

	audio := []byte("fake-audio-data")
	store := catalogtest.NewAudioStore()
	repo, router := mountStreamRoutes(store)
	track := makeReadyTrack(testUserId, "Whole", "Artist", "Album", "audio/whole.opus")
	repo.Seed(track)
	store.Seed("audio/whole.opus", audio)

	serve(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil)

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

// TestHandleStreamAudio_ClientAbortIsNotAServerError keeps the warn level
// meaningful: a player abandons a Range read on every seek and skip, and those
// cancellations reach the store as read errors.
func TestHandleStreamAudio_ClientAbortIsNotAServerError(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	store := &truncatingAudioStore{AudioStore: catalogtest.NewAudioStore(), failAfter: 1024}
	repo, router := mountStreamRoutes(store)
	track := makeReadyTrack(testUserId, "Skipped", "Artist", "Album", "audio/skipped.opus")
	repo.Seed(track)
	store.Seed("audio/skipped.opus", bytes.Repeat([]byte{0x5a}, 4096))

	ctx, abort := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/audio", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer fake-token")
	abort()
	router.ServeHTTP(httptest.NewRecorder(), req)

	served := findLogRecord(t, ring, "stream.served")
	if served.Attrs["outcome"] != "aborted" {
		t.Errorf("outcome = %q, want aborted", served.Attrs["outcome"])
	}
	if served.Level != slog.LevelInfo.String() {
		t.Errorf("level = %q, want %q: a client hanging up is not this server failing", served.Level, slog.LevelInfo)
	}
}

// TestHandleStreamAudio_FramesPrivateAudio pins #2199: the bytes are one
// user's private audio behind a bearer token, and the range arm proves the
// caching choice keeps seeking (206) working.
func TestHandleStreamAudio_FramesPrivateAudio(t *testing.T) {
	store := catalogtest.NewAudioStore()
	repo, router := mountStreamRoutes(store)
	track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/framed.opus")
	repo.Seed(track)
	store.Seed("audio/framed.opus", bytes.Repeat([]byte{0x5a}, 4096))
	path := "/tracks/" + track.ID.UUID().String() + "/audio"

	whole := serve(t, router, http.MethodGet, path, nil)
	ranged := httptest.NewRecorder()
	rangeReq := httptest.NewRequest(http.MethodGet, path, nil)
	rangeReq.Header.Set("Authorization", "Bearer fake-token")
	rangeReq.Header.Set("Range", "bytes=1024-2047")
	router.ServeHTTP(ranged, rangeReq)

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

func assertPrivateAudioHeaders(t *testing.T, rec *httptest.ResponseRecorder, wantCacheControl string) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, wantCacheControl)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

const (
	audioRouteDeadline = 150 * time.Millisecond
	audioSize          = 2 << 20
)

// serveAudioBehindRouteDeadline mounts the audio route behind the router-wide
// write deadline, as production does, on a real server, and returns a func that
// requests a seeded 2MB track with a slow-reading client.
func serveAudioBehindRouteDeadline(t *testing.T, idle time.Duration) (get func() *http.Response) {
	t.Helper()
	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	track := makeReadyTrack(testUserId, "Long", "Artist", "Album", "audio/long.opus")
	repo.Seed(track)
	store.Seed("audio/long.opus", bytes.Repeat([]byte{0xAB}, audioSize))

	h := NewStreamHandler(service.NewStreamTrackService(repo, store))
	h.writeIdleTimeout = idle
	r := chi.NewRouter()
	r.Use(httputil.WriteDeadline(audioRouteDeadline))
	r.Use(httputil.RequestLogger)
	r.Use(auth.Middleware(verifyAsTestUser))
	h.Routes(r)
	srv := httputiltest.NewServer(t, r)

	path := "/tracks/" + track.ID.UUID().String() + "/audio"
	return func() *http.Response {
		conn, br := httputiltest.Get(t, srv, path, http.Header{"Authorization": {"Bearer fake-token"}})
		resp := httputiltest.ReadResponse(t, conn, br)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		return resp
	}
}

// TestHandleStreamAudio_SlowNetworkIsNotCutOffByRouteDeadline guards #1018: a
// track that takes far longer than the API write deadline to download over a
// slow but steady connection must still arrive whole.
func TestHandleStreamAudio_SlowNetworkIsNotCutOffByRouteDeadline(t *testing.T) {
	get := serveAudioBehindRouteDeadline(t, time.Second)
	resp := get()
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
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(began)
	if got != audioSize {
		t.Fatalf("received %d of %d bytes after %s: slow download was cut off", got, audioSize, elapsed)
	}
	if elapsed <= 3*audioRouteDeadline {
		t.Fatalf("download took %s, not well past the %s route deadline, so it proves nothing", elapsed, audioRouteDeadline)
	}
}

// TestHandleStreamAudio_StalledClientIsCutOff proves the audio route is still
// bounded: a client that stops reading for longer than the idle timeout loses
// the connection instead of pinning the handler.
func TestHandleStreamAudio_StalledClientIsCutOff(t *testing.T) {
	const idle = 150 * time.Millisecond
	get := serveAudioBehindRouteDeadline(t, idle)
	resp := get()
	defer func() { _ = resp.Body.Close() }()

	time.Sleep(10 * idle)
	body, err := io.ReadAll(resp.Body)
	if err == nil && len(body) == audioSize {
		t.Fatalf("stalled client still received the whole %d-byte body: no idle write deadline", audioSize)
	}
}
