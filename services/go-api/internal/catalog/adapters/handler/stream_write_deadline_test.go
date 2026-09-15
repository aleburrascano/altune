package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/httputil/httputiltest"
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

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
