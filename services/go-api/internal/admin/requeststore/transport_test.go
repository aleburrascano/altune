package requeststore

import (
	"altune/go-api/internal/shared/logging"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fakeRT struct {
	resp *http.Response
	err  error
}

func (f fakeRT) RoundTrip(*http.Request) (*http.Response, error) { return f.resp, f.err }

func respWith(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}

func reqWithCorr(id string) *http.Request {
	r, _ := http.NewRequest("GET", "https://api/x", nil)
	if id != "" {
		r = r.WithContext(logging.WithCorrelationID(r.Context(), id))
	}
	return r
}

func TestTransport_PassthroughWithoutCorrID(t *testing.T) {
	s := New()
	base := respWith("hi")
	t.Cleanup(func() { _ = base.Body.Close() })
	rt := NewCorrelatedTransport(fakeRT{resp: base}, s)

	resp, err := rt.RoundTrip(reqWithCorr(""))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(s.Snapshot()) != 0 {
		t.Error("uncorrelated request must not be recorded")
	}
}

func TestTransport_RecordsAndDeliversFullBody(t *testing.T) {
	s := New()
	base := respWith("full-body-bytes")
	t.Cleanup(func() { _ = base.Body.Close() })
	rt := NewCorrelatedTransport(fakeRT{resp: base}, s)

	resp, _ := rt.RoundTrip(reqWithCorr("c1"))
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "full-body-bytes" {
		t.Fatalf("caller body = %q, want full", got)
	}
	if _, ok := s.Get("c1"); ok {
		t.Error("exchange should not be recorded until Close")
	}
	_ = resp.Body.Close()

	rec, ok := s.Get("c1")
	if !ok || len(rec.Exchanges) != 1 {
		t.Fatalf("expected one recorded exchange, got %+v", rec)
	}
	if rec.Exchanges[0].RespBody != "full-body-bytes" || rec.Exchanges[0].Truncated {
		t.Errorf("captured body = %q trunc=%v", rec.Exchanges[0].RespBody, rec.Exchanges[0].Truncated)
	}
}

func TestTransport_CapsBodyAndFlagsTruncated(t *testing.T) {
	s := New()
	s.maxBody = 4
	base := respWith("0123456789")
	t.Cleanup(func() { _ = base.Body.Close() })
	rt := NewCorrelatedTransport(fakeRT{resp: base}, s)

	resp, _ := rt.RoundTrip(reqWithCorr("c1"))
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "0123456789" {
		t.Fatalf("caller must still receive full body, got %q", got)
	}
	_ = resp.Body.Close()

	rec, _ := s.Get("c1")
	if rec.Exchanges[0].RespBody != "0123" || !rec.Exchanges[0].Truncated {
		t.Errorf("captured = %q trunc=%v, want \"0123\" truncated", rec.Exchanges[0].RespBody, rec.Exchanges[0].Truncated)
	}
}

// lockedBody is an inner body that is itself safe to Read and Close
// concurrently, so the race detector can only report capturingBody's own state.
type lockedBody struct {
	mu   sync.Mutex
	data *strings.Reader
}

func (b *lockedBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Read(p)
}

func (b *lockedBody) Close() error { return nil }

// TestTransport_ConcurrentReadAndCloseRecordsExactlyOnce pins that a body read
// on one goroutine while another closes it — and a second Close arriving at the
// same time — neither races on the capture buffer nor records twice. Run under
// -race; the store's own lock does not cover capturingBody's fields.
func TestTransport_ConcurrentReadAndCloseRecordsExactlyOnce(t *testing.T) {
	for range 20 {
		assertOneExchangeAfterConcurrentReadAndClose(t)
	}
}

func assertOneExchangeAfterConcurrentReadAndClose(t *testing.T) {
	t.Helper()
	s := New()
	inner := &lockedBody{data: strings.NewReader(strings.Repeat("x", 64*1024))}
	rt := NewCorrelatedTransport(fakeRT{resp: &http.Response{StatusCode: 200, Body: inner}}, s)
	resp, err := rt.RoundTrip(reqWithCorr("c1"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	readWhileClosingTwice(resp.Body)

	rec, ok := s.Get("c1")
	if !ok || len(rec.Exchanges) != 1 {
		t.Fatalf("recorded %d exchanges (found=%v), want exactly 1", len(rec.Exchanges), ok)
	}
}

func readWhileClosingTwice(body io.ReadCloser) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, act := range []func(){
		func() { _, _ = io.Copy(io.Discard, body) },
		func() { _ = body.Close() },
		func() { _ = body.Close() },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			act()
		}()
	}
	close(start)
	wg.Wait()
}

func TestTransport_RecordsTransportError(t *testing.T) {
	s := New()
	rt := NewCorrelatedTransport(fakeRT{err: errors.New("dial timeout")}, s)

	resp, err := rt.RoundTrip(reqWithCorr("c1"))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	rec, ok := s.Get("c1")
	if !ok || rec.Exchanges[0].Err == "" {
		t.Errorf("transport error should be recorded, got %+v", rec)
	}
}
