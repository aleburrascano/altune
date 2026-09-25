package requeststore

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b failingBody) Close() error             { return nil }

type failingBodyTransport struct{ err error }

func (f failingBodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: failingBody(f)}, nil
}

func TestRerunRecorderRedactsSecretInBodyReadError(t *testing.T) {
	secret := "hunter2secret"
	readErr := errors.New("read failed: https://example.test/x?access_token=" + secret)
	recorder := NewRerunRecorder(failingBodyTransport{err: readErr}, 1024)
	req, err := http.NewRequest(http.MethodGet, "https://example.test/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := recorder.RoundTrip(req)
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	exchanges := recorder.Exchanges()
	if len(exchanges) != 1 || exchanges[0].Err == "" {
		t.Fatalf("exchange Err not recorded: %+v", exchanges)
	}
	if strings.Contains(exchanges[0].Err, secret) {
		t.Fatalf("secret leaked in Err: %q", exchanges[0].Err)
	}
}
