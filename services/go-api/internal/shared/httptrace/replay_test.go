package httptrace

import (
	"io"
	"net/http"
	"testing"
)

func mustGet(t *testing.T, rt http.RoundTripper, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip %s: %v", url, err)
	}
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return string(b)
}

func TestReplayer_ReturnsRecordedResponse(t *testing.T) {
	r := NewReplayer([]Exchange{
		{Method: "GET", URL: "https://api.example.com/search?q=foo", Status: 200, RespBody: `{"hit":"foo"}`},
	})

	resp := mustGet(t, r, "https://api.example.com/search?q=foo")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyString(t, resp); got != `{"hit":"foo"}` {
		t.Errorf("body = %q, want the recorded body", got)
	}
}

func TestReplayer_FIFOForRepeatedKey(t *testing.T) {
	r := NewReplayer([]Exchange{
		{Method: "GET", URL: "https://api.example.com/page", Status: 200, RespBody: `first`},
		{Method: "GET", URL: "https://api.example.com/page", Status: 200, RespBody: `second`},
	})

	if got := bodyString(t, mustGet(t, r, "https://api.example.com/page")); got != "first" {
		t.Errorf("first replay = %q, want first", got)
	}
	if got := bodyString(t, mustGet(t, r, "https://api.example.com/page")); got != "second" {
		t.Errorf("second replay = %q, want second", got)
	}
}

func TestReplayer_MissIsHardError(t *testing.T) {
	r := NewReplayer(nil)

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/missing", nil)
	if _, err := r.RoundTrip(req); err == nil {
		t.Fatal("expected an error for an unrecorded request, got nil")
	}
}

func TestReplayer_RemainingTracksUnreplayed(t *testing.T) {
	r := NewReplayer([]Exchange{
		{Method: "GET", URL: "https://api.example.com/a", Status: 200, RespBody: `a`},
		{Method: "GET", URL: "https://api.example.com/b", Status: 200, RespBody: `b`},
	})
	if r.Remaining() != 2 {
		t.Fatalf("Remaining before = %d, want 2", r.Remaining())
	}
	_ = bodyString(t, mustGet(t, r, "https://api.example.com/a"))
	if r.Remaining() != 1 {
		t.Errorf("Remaining after one replay = %d, want 1", r.Remaining())
	}
}

func TestReplayer_RecordedTransportErrorReplaysAsError(t *testing.T) {
	r := NewReplayer([]Exchange{
		{Method: "GET", URL: "https://api.example.com/down", Err: "connection refused"},
	})
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/down", nil)
	if _, err := r.RoundTrip(req); err == nil {
		t.Fatal("expected the recorded transport error to replay as an error")
	}
}
