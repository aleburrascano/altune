package requeststore

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// countingBody serves size bytes of 'x' and counts how many the reader consumed.
type countingBody struct {
	remaining int
	consumed  int
	closed    bool
}

func (c *countingBody) Read(p []byte) (int, error) {
	if c.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), c.remaining)
	for i := range n {
		p[i] = 'x'
	}
	c.remaining -= n
	c.consumed += n
	return n, nil
}

func (c *countingBody) Close() error {
	c.closed = true
	return nil
}

func rerunRoundTrip(t *testing.T, body io.ReadCloser, bodyCap int) (*RerunRecorder, *http.Response) {
	t.Helper()
	rr := NewRerunRecorder(fakeRT{resp: &http.Response{StatusCode: 200, Body: body}}, bodyCap)
	req, err := http.NewRequest("GET", "https://api/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	return rr, resp
}

func TestRerunRecorder_BoundsReadToBodyCap(t *testing.T) {
	const bodyCap, size = 1024, 10 << 20
	body := &countingBody{remaining: size}
	rr, resp := rerunRoundTrip(t, body, bodyCap)
	t.Cleanup(func() { _ = resp.Body.Close() })

	if limit := bodyCap + 1; body.consumed > limit {
		t.Fatalf("RoundTrip consumed %d bytes of upstream body, want <= %d", body.consumed, limit)
	}
	ex := rr.Exchanges()[0]
	if len(ex.RespBody) != bodyCap || !ex.Truncated {
		t.Fatalf("captured %d bytes truncated=%v, want %d truncated=true", len(ex.RespBody), ex.Truncated, bodyCap)
	}
}

func TestRerunRecorder_CallerReceivesFullBodyPastCap(t *testing.T) {
	const bodyCap, size = 16, 5000
	body := &countingBody{remaining: size}
	_, resp := rerunRoundTrip(t, body, bodyCap)

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte("x"), size)) {
		t.Fatalf("caller got %d bytes, want the full %d-byte stream", len(got), size)
	}
	if err := resp.Body.Close(); err != nil || !body.closed {
		t.Fatalf("closing caller body must close upstream body (err=%v closed=%v)", err, body.closed)
	}
}

func TestRerunRecorder_BodyWithinCapNotTruncated(t *testing.T) {
	body := &countingBody{remaining: 16}
	rr, resp := rerunRoundTrip(t, body, 16)
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	ex := rr.Exchanges()[0]
	if len(got) != 16 || len(ex.RespBody) != 16 || ex.Truncated || ex.Status != 200 {
		t.Fatalf("got=%d captured=%d truncated=%v status=%d", len(got), len(ex.RespBody), ex.Truncated, ex.Status)
	}
}
