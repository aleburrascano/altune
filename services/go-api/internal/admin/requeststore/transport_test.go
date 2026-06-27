package requeststore

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"altune/go-api/internal/shared/httputil"
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
		r = r.WithContext(httputil.WithCorrelationID(r.Context(), id))
	}
	return r
}

func TestTransport_PassthroughWithoutCorrID(t *testing.T) {
	s := New()
	rt := NewTransport(fakeRT{resp: respWith("hi")}, s)

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
	rt := NewTransport(fakeRT{resp: respWith("full-body-bytes")}, s)

	resp, _ := rt.RoundTrip(reqWithCorr("c1"))
	got, _ := io.ReadAll(resp.Body) // caller still gets the whole body
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
	rt := NewTransport(fakeRT{resp: respWith("0123456789")}, s)

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

func TestTransport_RecordsTransportError(t *testing.T) {
	s := New()
	rt := NewTransport(fakeRT{err: errors.New("dial timeout")}, s)

	if _, err := rt.RoundTrip(reqWithCorr("c1")); err == nil {
		t.Fatal("expected error to propagate")
	}
	rec, ok := s.Get("c1")
	if !ok || rec.Exchanges[0].Err == "" {
		t.Errorf("transport error should be recorded, got %+v", rec)
	}
}
