package transport_test

import (
	"altune/go-api/internal/shared/httptrace/internal/adapters/transport"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
)

type canned struct{ body io.ReadCloser }

func (c canned) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: c.body}, nil
}

func record(t *testing.T, body io.ReadCloser) transport.Exchange {
	t.Helper()
	rec := transport.NewRecorder(canned{body: body})
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/x", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip = %v", err)
	}
	_ = resp.Body.Close()
	return rec.Exchanges()[0]
}

func TestRecorder_ARespondedBodyIsRecordedWithoutAnError(t *testing.T) {
	ex := record(t, io.NopCloser(strings.NewReader("hello")))

	if ex.RespBody != "hello" || ex.Err != "" {
		t.Fatalf("exchange = %+v, want body hello and no error", ex)
	}
}

func TestRecorder_ABodyThatFailsMidReadIsRecordedWithTheReasonItBroke(t *testing.T) {
	cause := errors.New("connection reset")
	ex := record(t, io.NopCloser(iotest.ErrReader(cause)))

	if !strings.Contains(ex.Err, "read a body for capture") || !strings.Contains(ex.Err, cause.Error()) {
		t.Fatalf("exchange error = %q, want it to say the capture read failed with %q", ex.Err, cause)
	}
}
