package httputil

import (
	"altune/go-api/internal/shared/httputil/httputiltest"
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

const (
	testWriteDeadline = 150 * time.Millisecond
	// cutoffWait is how long a test waits for a write to be cut off. Without a
	// deadline the write blocks until the client goes away, so exceeding it is
	// the failure signal.
	cutoffWait = 3 * time.Second
)

// writeUntilError writes chunks to w until a write fails or giveUp elapses,
// then reports the error (nil if it gave up).
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

// deadlineStack mirrors production ordering: WriteDeadline wraps the request
// logger, which wraps the handler.
func deadlineStack(h http.HandlerFunc) http.Handler {
	return WriteDeadline(testWriteDeadline)(RequestLogger(h))
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
	srv := httputiltest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		start <- time.Now()
		errs <- writeUntilError(w, 2*cutoffWait)
	}))

	httputiltest.Get(t, srv, "/", nil) // never reads

	began := <-start
	awaitWriteError(t, errs)
	if elapsed := time.Since(began); elapsed < testWriteDeadline {
		t.Errorf("write cut off after %s, before the %s deadline", elapsed, testWriteDeadline)
	}
}

func TestClearWriteDeadline_StreamOutlivesRouteDeadline(t *testing.T) {
	srv := httputiltest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		ClearWriteDeadline(w)
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		time.Sleep(3 * testWriteDeadline)
		_, _ = io.WriteString(w, "late frame")
		_ = rc.Flush()
	}))

	conn, br := httputiltest.Get(t, srv, "/", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "late frame" {
		t.Fatalf("body = %q, err = %v; want the frame written after the route deadline", body, err)
	}
}

func TestExtendWriteDeadlineOnWrite_SlowButSteadyClientGetsWholeBody(t *testing.T) {
	// The client drains 32KB every 20ms, so the body (far larger than the
	// socket buffers) takes over a second: several times the route deadline,
	// while every individual write completes well inside the idle timeout.
	const size = 2 << 20
	srv := httputiltest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		ew := ExtendWriteDeadlineOnWrite(w, time.Second)
		_, _ = io.Copy(ew, io.LimitReader(zeroReader{}, size))
	}))

	conn, br := httputiltest.Get(t, srv, "/", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
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
	if got != size {
		t.Fatalf("received %d of %d bytes after %s: steady slow client was cut off", got, size, time.Since(began))
	}
	if elapsed := time.Since(began); elapsed <= 3*testWriteDeadline {
		t.Fatalf("transfer took %s, not longer than the %s route deadline, so it proves nothing", elapsed, testWriteDeadline)
	}
}

func TestExtendWriteDeadlineOnWrite_CutsOffStalledClient(t *testing.T) {
	errs := make(chan error, 1)
	srv := httputiltest.NewServer(t, deadlineStack(func(w http.ResponseWriter, _ *http.Request) {
		errs <- writeUntilError(ExtendWriteDeadlineOnWrite(w, testWriteDeadline), 2*cutoffWait)
	}))

	httputiltest.Get(t, srv, "/", nil) // never reads

	awaitWriteError(t, errs)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
