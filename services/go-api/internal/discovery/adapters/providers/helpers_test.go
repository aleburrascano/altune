package providers

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// hostileMBID carries every URL-special character that could restructure an
// outbound request if interpolated raw: a path separator, a query start and a
// fragment start. MBIDs come verbatim from third-party JSON (issue #572).
const hostileMBID = "a/b?c#d"

// capturingClient records the outbound request and answers with an empty 404,
// so nothing leaves the process.
func capturingClient(capture **http.Request) *http.Client {
	return &http.Client{Transport: fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		*capture = r
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}}}
}

// sensitiveQuery is distinctive enough that any substring hit in captured log
// output can only come from the user's search text.
const sensitiveQuery = "zqxj private diagnosis clinic"

// captureDefaultLog routes the default slog logger into a JSON buffer, as the
// production stdout handler does, and restores it when the test ends.
func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func assertNoQueryText(t *testing.T, logged string) {
	t.Helper()
	for _, form := range []string{sensitiveQuery, url.QueryEscape(sensitiveQuery), url.PathEscape(sensitiveQuery), "diagnosis"} {
		if strings.Contains(logged, form) {
			t.Fatalf("log output contains search text %q:\n%s", form, logged)
		}
	}
}
