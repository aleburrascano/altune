package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetJSON_non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dst map[string]any
	err := getJSON(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil || !strings.Contains(err.Error(), "http status 500") {
		t.Fatalf("err = %v, want an http status 500 error", err)
	}
}

func TestGetJSON_malformedBodyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	var dst map[string]any
	if err := getJSON(context.Background(), srv.Client(), srv.URL, &dst); err == nil {
		t.Fatal("expected a decode error on an HTML-instead-of-JSON body, got nil")
	}
}

func TestGetBytes_non200ReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"slow down"}`))
	}))
	defer srv.Close()

	status, body, err := getBytes(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected an error on 429, got nil")
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 (callers branch on it)", status)
	}
	if !strings.Contains(string(body), "slow down") {
		t.Errorf("body = %q, want the 429 body returned for inspection", body)
	}
}

func TestGetBytesCapped_capsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	status, body, err := getBytesCapped(context.Background(), srv.Client(), srv.URL, 10)
	if err != nil {
		t.Fatalf("getBytesCapped: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 10 {
		t.Errorf("len(body) = %d, want the 10-byte cap applied", len(body))
	}
}

func TestGetBytes_transportErrorZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	status, _, err := getBytes(context.Background(), http.DefaultClient, srv.URL)
	if err == nil {
		t.Fatal("expected a transport error against a closed server")
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 on a transport error", status)
	}
}

func TestPostJSON_non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dst map[string]any
	status, err := postJSON(context.Background(), srv.Client(), srv.URL, []byte(`{}`), &dst)
	if err == nil || !strings.Contains(err.Error(), "http status 500") {
		t.Fatalf("err = %v, want an http status 500 error", err)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (callers branch on it)", status)
	}
}

func TestPostJSON_forwardsBodyAndDecodes(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var dst struct {
		OK bool `json:"ok"`
	}
	status, err := postJSON(context.Background(), srv.Client(), srv.URL, []byte(`{"q":1}`), &dst)
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if posted != `{"q":1}` {
		t.Errorf("posted body = %q, want the payload forwarded", posted)
	}
	if !dst.OK {
		t.Error("want the response decoded into dst")
	}
}

func TestPostBytesCapped_non200ReturnsStatusAndBodyWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	}))
	defer srv.Close()

	status, body, err := postBytesCapped(context.Background(), srv.Client(), srv.URL, strings.NewReader("q"), 1<<20)
	if err != nil {
		t.Fatalf("postBytesCapped must not gate on status (callers branch on it): %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	if !strings.Contains(string(body), "boom") {
		t.Errorf("body = %q, want the 500 body returned for inspection", body)
	}
}

func TestPostBytesCappedOK_non200IsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	status, _, err := postBytesCappedOK(context.Background(), srv.Client(), srv.URL, strings.NewReader("q"), 1<<20)
	if err == nil || err.Error() != "http status 502" {
		t.Fatalf("err = %v, want exactly %q", err, "http status 502")
	}
	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", status)
	}
}

func TestPostBytesCappedOK_200ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	status, body, err := postBytesCappedOK(context.Background(), srv.Client(), srv.URL, nil, 1<<20)
	if err != nil {
		t.Fatalf("postBytesCappedOK: %v", err)
	}
	if status != http.StatusOK || string(body) != "ok" {
		t.Errorf("status, body = %d, %q, want 200, %q", status, body, "ok")
	}
}

func TestPostBytesCappedOK_transportErrorHasZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	status, _, err := postBytesCappedOK(context.Background(), http.DefaultClient, srv.URL, nil, 1<<20)
	if err == nil || strings.Contains(err.Error(), "http status") {
		t.Fatalf("err = %v, want the transport error, not a status error", err)
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 when no response arrived", status)
	}
}

func TestPostBytesCapped_capsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	status, body, err := postBytesCapped(context.Background(), srv.Client(), srv.URL, nil, 10)
	if err != nil {
		t.Fatalf("postBytesCapped: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 10 {
		t.Errorf("len(body) = %d, want the 10-byte cap applied", len(body))
	}
}

// providerKeyInQuery stands for the credential Last.fm, fanart.tv and
// SoundCloud pass in the query string of every call.
const providerKeyInQuery = "0123456789abcdeflastfmkey"

// closedMidRequestServer accepts the request, then drops the connection without
// answering — the transport failure that makes net/http return a *url.Error
// echoing the full request URL.
func closedMidRequestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
}

// requestHelpers is every helper here that reaches the network, so the credential
// scrub is proven at each call site rather than at the one a reader happened to
// open.
func requestHelpers() map[string]func(context.Context, *http.Client, string) error {
	return map[string]func(context.Context, *http.Client, string) error{
		"getJSON": func(ctx context.Context, c *http.Client, u string) error {
			var dst map[string]any
			return getJSON(ctx, c, u, &dst)
		},
		"getBytes": func(ctx context.Context, c *http.Client, u string) error {
			_, _, err := getBytes(ctx, c, u)
			return err
		},
		"postJSON": func(ctx context.Context, c *http.Client, u string) error {
			var dst map[string]any
			_, err := postJSON(ctx, c, u, []byte(`{}`), &dst)
			return err
		},
		"postBytesCapped": func(ctx context.Context, c *http.Client, u string) error {
			_, _, err := postBytesCapped(ctx, c, u, strings.NewReader("q"), 1<<20)
			return err
		},
	}
}

// TestRequestHelpers_TransportErrorDropsQueryCredential pins #2227: net/http
// embeds the full request URL in the *url.Error a transport failure returns, so
// every call site logging that error raw put the provider key in the persisted
// stdout log.
func TestRequestHelpers_TransportErrorDropsQueryCredential(t *testing.T) {
	srv := closedMidRequestServer(t)
	defer srv.Close()
	target := srv.URL + "/2.0/?method=artist.getinfo&api_key=" + providerKeyInQuery

	for name, call := range requestHelpers() {
		t.Run(name, func(t *testing.T) {
			err := call(context.Background(), srv.Client(), target)
			if err == nil {
				t.Fatal("expected a transport error when the connection closes mid-request")
			}
			if strings.Contains(err.Error(), providerKeyInQuery) {
				t.Errorf("provider key survived in the error text: %v", err)
			}
			if !strings.Contains(err.Error(), "/2.0/") {
				t.Errorf("host and path must survive for diagnosis: %v", err)
			}
		})
	}
}

func TestWithHeader_emptyValueNotSet(t *testing.T) {
	var gotUA string
	var uaPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, uaPresent = r.Header["X-Custom"]
		gotUA = r.Header.Get("X-Other")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var dst map[string]any
	err := getJSON(context.Background(), srv.Client(), srv.URL, &dst,
		withHeader("X-Custom", ""),
		withHeader("X-Other", "set"))
	if err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if uaPresent {
		t.Error("withHeader with an empty value must not set the header")
	}
	if gotUA != "set" {
		t.Errorf("X-Other = %q, want %q", gotUA, "set")
	}
}

// oversizedJSONServer serves a single well-formed JSON object whose total
// length exceeds providerBodyCap.
func oversizedJSONServer(t *testing.T) *httptest.Server {
	t.Helper()
	payload := `{"blob":"` + strings.Repeat("x", int(providerBodyCap)+1024) + `"}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

func TestGetJSON_oversizedBodyIsRejected(t *testing.T) {
	srv := oversizedJSONServer(t)
	defer srv.Close()

	var dst struct {
		Blob string `json:"blob"`
	}
	err := getJSON(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil {
		t.Fatalf("getJSON decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), providerBodyCap)
	}
	if len(dst.Blob) > int(providerBodyCap) {
		t.Errorf("len(dst.Blob) = %d, want nothing beyond the %d-byte cap buffered", len(dst.Blob), providerBodyCap)
	}
}

func TestGetJSONWithStatus_okDecodesAndReturnsStatus(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test")
		_, _ = w.Write([]byte(`{"k":"v"}`))
	}))
	defer srv.Close()

	var dst map[string]string
	status, err := getJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst, withHeader("X-Test", "yes"))
	if err != nil {
		t.Fatalf("getJSONWithStatus: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if dst["k"] != "v" {
		t.Errorf("dst = %v, want k=v decoded", dst)
	}
	if gotHeader != "yes" {
		t.Errorf("X-Test = %q, want the reqOption applied", gotHeader)
	}
}

func TestGetJSONWithStatus_non200ReturnsStatusAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var dst map[string]any
	status, err := getJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil || err.Error() != "http status 401" {
		t.Fatalf("err = %v, want http status 401", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (auth-retry callers branch on it)", status)
	}
}

func TestGetJSONWithStatus_malformedBodyReturns200AndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	var dst map[string]any
	status, err := getJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil {
		t.Fatal("expected a decode error, got nil")
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (decode failure is not a status failure)", status)
	}
}

func TestGetJSONWithStatus_transportErrorReturnsZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	u := srv.URL
	srv.Close()

	var dst map[string]any
	status, err := getJSONWithStatus(context.Background(), client, u, &dst)
	if err == nil {
		t.Fatal("expected a transport error against a closed server, got nil")
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 when no response arrived", status)
	}
}

func TestGetJSONWithStatus_oversizedBodyIsRejected(t *testing.T) {
	srv := oversizedJSONServer(t)
	defer srv.Close()

	var dst struct {
		Blob string `json:"blob"`
	}
	status, err := getJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil {
		t.Fatalf("decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), providerBodyCap)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (the cap is a body failure, not a status one)", status)
	}
}

func TestPostJSON_oversizedBodyIsRejected(t *testing.T) {
	srv := oversizedJSONServer(t)
	defer srv.Close()

	var dst struct {
		Blob string `json:"blob"`
	}
	status, err := postJSON(context.Background(), srv.Client(), srv.URL, []byte(`{}`), &dst)
	if err == nil {
		t.Fatalf("postJSON decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), providerBodyCap)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (the cap is a body failure, not a status one)", status)
	}
	if len(dst.Blob) > int(providerBodyCap) {
		t.Errorf("len(dst.Blob) = %d, want nothing beyond the %d-byte cap buffered", len(dst.Blob), providerBodyCap)
	}
}
