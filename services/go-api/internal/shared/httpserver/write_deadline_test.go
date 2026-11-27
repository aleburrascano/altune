package httpserver_test

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/slowclienttest"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	testWriteDeadline = 150 * time.Millisecond
	cutoffWait        = 3 * time.Second
	deadlineSlack     = 25 * time.Millisecond
)

func writeUntilError(w io.Writer, giveUp time.Duration) error {
	chunk := bytes.Repeat([]byte("x"), 16<<10)
	stop := time.Now().Add(giveUp)
	for time.Now().Before(stop) {
		if _, err := w.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}

func deadlineStack(h http.HandlerFunc) http.Handler {
	return httpserver.WriteDeadline(clock.Now, testWriteDeadline)(httpserver.RequestLogger(clock.Now)(h))
}

func awaitWriteError(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("handler write never failed: slow-reading client held it open")
		}
	case <-time.After(cutoffWait):
		t.Fatalf("handler write still blocked after %s: no write deadline applied", cutoffWait)
	}
}

func TestWriteDeadline_CutsOffClientThatStopsReading(t *testing.T) {
	errs := make(chan error, 1)
	start := make(chan time.Time, 1)
	srv := slowclienttest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		start <- time.Now()
		errs <- writeUntilError(w, 2*cutoffWait)
	}))

	slowclienttest.Get(t, srv, "/", nil)

	began := <-start
	awaitWriteError(t, errs)
	if elapsed := time.Since(began); elapsed < testWriteDeadline-deadlineSlack {
		t.Errorf("write cut off after %s, more than %s before the %s deadline", elapsed, deadlineSlack, testWriteDeadline)
	}
}

func TestClearWriteDeadline_StreamOutlivesRouteDeadline(t *testing.T) {
	deadlinePassed := make(chan struct{})
	srv := slowclienttest.NewServer(t, deadlineStack(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/probe" {
			_ = writeUntilError(w, 2*cutoffWait)
			close(deadlinePassed)
			return
		}
		httpserver.ClearWriteDeadline(w)
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		select {
		case <-deadlinePassed:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "late frame")
		_ = rc.Flush()
	}))

	conn, br := slowclienttest.Get(t, srv, "/", nil)
	slowclienttest.Get(t, srv, "/probe", nil)
	resp := slowclienttest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "late frame" {
		t.Fatalf("body = %q, err = %v; want the frame written after the route deadline", body, err)
	}
}

func TestExtendWriteDeadlineOnWrite_SlowButSteadyClientGetsWholeBody(t *testing.T) {
	const size = 2 << 20
	srv := slowclienttest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		ew := httpserver.ExtendWriteDeadlineOnWrite(clock.Now, w, time.Second)
		_, _ = io.Copy(ew, io.LimitReader(zeroReader{}, size))
	}))

	conn, br := slowclienttest.Get(t, srv, "/", nil)
	resp := slowclienttest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	began := time.Now()
	got := 0
	buf := make([]byte, 32<<10)
	pace := time.NewTicker(20 * time.Millisecond)
	defer pace.Stop()
	for {
		n, err := resp.Body.Read(buf)
		got += n
		if err != nil {
			break
		}
		<-pace.C
	}
	if got != size {
		t.Fatalf("received %d of %d bytes after %s: steady slow client was cut off", got, size, time.Since(began))
	}
	if elapsed := time.Since(began); elapsed <= 3*testWriteDeadline {
		t.Fatalf("transfer took %s, not longer than the %s route deadline, so it proves nothing", elapsed, testWriteDeadline)
	}
}

func TestExtendWriteDeadlineOnWrite_CutsOffStalledClient(t *testing.T) {
	errs := make(chan error, 1)
	srv := slowclienttest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		errs <- writeUntilError(httpserver.ExtendWriteDeadlineOnWrite(clock.Now, w, testWriteDeadline), 2*cutoffWait)
	}))

	slowclienttest.Get(t, srv, "/", nil)

	awaitWriteError(t, errs)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestWriteDeadline_RefusesAMissingClock(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "WriteDeadline") {
			t.Errorf("panic = %q, want a refusal naming WriteDeadline", msg)
		}
	}()
	httpserver.WriteDeadline(nil, time.Second)
}

func TestExtendWriteDeadlineOnWrite_RefusesAMissingClock(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "ExtendWriteDeadlineOnWrite") {
			t.Errorf("panic = %q, want a refusal naming ExtendWriteDeadlineOnWrite", msg)
		}
	}()
	httpserver.ExtendWriteDeadlineOnWrite(nil, nil, time.Second)
}
