package httputiltest_test

import (
	"altune/go-api/internal/shared/httputiltest"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGet_SendsPathAndHeadersAndReadResponseReadsTheReply(t *testing.T) {
	srv := httputiltest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", r.URL.Path+" "+strings.Join(r.Header.Values("X-Probe"), ","))
		_, _ = io.WriteString(w, "hello")
	}))

	conn, br := httputiltest.Get(t, srv, "/probe", http.Header{"X-Probe": {"a", "b"}})
	resp := httputiltest.ReadResponse(t, conn, br)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("status, body = %d, %q; want 200, hello", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Echo"); got != "/probe a,b" {
		t.Fatalf("server saw %q, want path /probe and both header values", got)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("closing the body must close the connection")
	}
}

func TestNewServer_WriteDeadlineFiresWhenTheClientStopsReading(t *testing.T) {
	writeErr := make(chan error, 1)
	srv := httputiltest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		chunk := make([]byte, 32<<10)
		var err error
		for written := 0; written < 1<<20 && err == nil; written += len(chunk) {
			_, err = w.Write(chunk)
		}
		writeErr <- err
	}))

	httputiltest.Get(t, srv, "/", nil)

	select {
	case err := <-writeErr:
		if err == nil {
			t.Fatal("1 MiB fit in the socket buffers; the write deadline must fire while the client is not reading")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never returned")
	}
}
