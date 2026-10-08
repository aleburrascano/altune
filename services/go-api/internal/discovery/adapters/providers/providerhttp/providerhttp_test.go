package providerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func TestGetJSON_non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dst map[string]any
	err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst)
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
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst); err == nil {
		t.Fatal("expected a decode error on an HTML-instead-of-JSON body, got nil")
	}
}

func TestGetBytes_non200ReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"slow down"}`))
	}))
	defer srv.Close()

	status, body, err := GetBytes(context.Background(), srv.Client(), srv.URL)
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

	status, body, err := GetBytesCapped(context.Background(), srv.Client(), srv.URL, 10)
	if err != nil {
		t.Fatalf("GetBytesCapped: %v", err)
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

	status, _, err := GetBytes(context.Background(), http.DefaultClient, srv.URL)
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
	status, err := PostJSON(context.Background(), srv.Client(), srv.URL, []byte(`{}`), &dst)
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
	status, err := PostJSON(context.Background(), srv.Client(), srv.URL, []byte(`{"q":1}`), &dst)
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
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

	status, body, err := PostBytesCapped(context.Background(), srv.Client(), srv.URL, strings.NewReader("q"), 1<<20)
	if err != nil {
		t.Fatalf("PostBytesCapped must not gate on status (callers branch on it): %v", err)
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

	status, _, err := PostBytesCappedOK(context.Background(), srv.Client(), srv.URL, strings.NewReader("q"), 1<<20)
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

	status, body, err := PostBytesCappedOK(context.Background(), srv.Client(), srv.URL, nil, 1<<20)
	if err != nil {
		t.Fatalf("PostBytesCappedOK: %v", err)
	}
	if status != http.StatusOK || string(body) != "ok" {
		t.Errorf("status, body = %d, %q, want 200, %q", status, body, "ok")
	}
}

func TestPostBytesCappedOK_transportErrorHasZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	status, _, err := PostBytesCappedOK(context.Background(), http.DefaultClient, srv.URL, nil, 1<<20)
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

	status, body, err := PostBytesCapped(context.Background(), srv.Client(), srv.URL, nil, 10)
	if err != nil {
		t.Fatalf("PostBytesCapped: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 10 {
		t.Errorf("len(body) = %d, want the 10-byte cap applied", len(body))
	}
}

const providerKeyInQuery = "0123456789abcdeflastfmkey"

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

func requestHelpers() map[string]func(context.Context, *http.Client, string) error {
	return map[string]func(context.Context, *http.Client, string) error{
		"GetJSON": func(ctx context.Context, c *http.Client, u string) error {
			var dst map[string]any
			return GetJSON(ctx, c, u, &dst)
		},
		"GetBytes": func(ctx context.Context, c *http.Client, u string) error {
			_, _, err := GetBytes(ctx, c, u)
			return err
		},
		"PostJSON": func(ctx context.Context, c *http.Client, u string) error {
			var dst map[string]any
			_, err := PostJSON(ctx, c, u, []byte(`{}`), &dst)
			return err
		},
		"PostBytesCapped": func(ctx context.Context, c *http.Client, u string) error {
			_, _, err := PostBytesCapped(ctx, c, u, strings.NewReader("q"), 1<<20)
			return err
		},
	}
}

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
	err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst,
		WithHeader("X-Custom", ""),
		WithHeader("X-Other", "set"))
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if uaPresent {
		t.Error("WithHeader with an empty value must not set the header")
	}
	if gotUA != "set" {
		t.Errorf("X-Other = %q, want %q", gotUA, "set")
	}
}

func oversizedJSONServer(t *testing.T) *httptest.Server {
	t.Helper()
	payload := `{"blob":"` + strings.Repeat("x", int(BodyCap)+1024) + `"}`
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
	err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil {
		t.Fatalf("GetJSON decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), BodyCap)
	}
	if len(dst.Blob) > int(BodyCap) {
		t.Errorf("len(dst.Blob) = %d, want nothing beyond the %d-byte cap buffered", len(dst.Blob), BodyCap)
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
	status, err := GetJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst, WithHeader("X-Test", "yes"))
	if err != nil {
		t.Fatalf("GetJSONWithStatus: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if dst["k"] != "v" {
		t.Errorf("dst = %v, want k=v decoded", dst)
	}
	if gotHeader != "yes" {
		t.Errorf("X-Test = %q, want the ReqOption applied", gotHeader)
	}
}

func TestGetJSONWithStatus_non200ReturnsStatusAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var dst map[string]any
	status, err := GetJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
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
	status, err := GetJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
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
	status, err := GetJSONWithStatus(context.Background(), client, u, &dst)
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
	status, err := GetJSONWithStatus(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil {
		t.Fatalf("decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), BodyCap)
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
	status, err := PostJSON(context.Background(), srv.Client(), srv.URL, []byte(`{}`), &dst)
	if err == nil {
		t.Fatalf("PostJSON decoded a %d-byte blob past the %d-byte cap; want the body capped and decode rejected", len(dst.Blob), BodyCap)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (the cap is a body failure, not a status one)", status)
	}
	if len(dst.Blob) > int(BodyCap) {
		t.Errorf("len(dst.Blob) = %d, want nothing beyond the %d-byte cap buffered", len(dst.Blob), BodyCap)
	}
}

func TestGetJSON_malformedBodyIsMarkedBadPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	var dst map[string]any
	err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst)
	marker, ok := err.(interface{ BadPayload() bool })
	if !ok || !marker.BadPayload() {
		t.Fatalf("err = %v, want a bad payload error", err)
	}
}

func TestGetJSON_bare403IsNotMarkedPersistent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	var dst map[string]any
	err := GetJSON(context.Background(), srv.Client(), srv.URL, &dst)
	if _, marked := err.(interface{ PersistentAuthFailure() bool }); err == nil || marked {
		t.Fatalf("err = %v, want a plain 403 without the persistent marker", err)
	}
}

func TestGetText_200ReturnsBodyAndSendsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	got, err := GetText(context.Background(), srv.Client(), srv.URL, 1024, "test-agent/1")
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v; want hello, nil", got, err)
	}
	if gotUA != "test-agent/1" {
		t.Fatalf("User-Agent = %q, want test-agent/1", gotUA)
	}
}

func TestGetText_503ReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := GetText(context.Background(), srv.Client(), srv.URL, 1024, "ua")
	want := "GET " + srv.URL + ": status 503"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestGetText_truncatesAtLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 11)))
	}))
	defer srv.Close()

	got, err := GetText(context.Background(), srv.Client(), srv.URL, 10, "ua")
	if err != nil || len(got) != 10 {
		t.Fatalf("len = %d, err = %v; want 10, nil", len(got), err)
	}
}

func TestGetText_emptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	got, err := GetText(context.Background(), srv.Client(), srv.URL, 1024, "ua")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v; want empty, nil", got, err)
	}
}

func TestNewHeadRequest_setsMethodAndOptions(t *testing.T) {
	req, err := NewHeadRequest(context.Background(), "https://example.test/x", WithHeader("Accept", "image/*"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodHead {
		t.Fatalf("method = %s, want HEAD", req.Method)
	}
	if got := req.Header.Get("Accept"); got != "image/*" {
		t.Fatalf("Accept = %q, want image/*", got)
	}
}

func TestArtworkFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantMiss bool
		wantIs   []error
	}{
		{name: "404 is a miss", err: StatusError{Status: 404}, wantMiss: true},
		{name: "400 is a miss", err: StatusError{Status: 400}, wantMiss: true},
		{name: "410 is refused", err: StatusError{Status: 410}, wantIs: []error{ports.ErrArtworkUnavailable}},
		{name: "context canceled", err: context.Canceled, wantIs: []error{ports.ErrArtworkUnavailable, context.Canceled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ArtworkFailure(domain.ProviderKeyCoverArtArchive, tt.err)
			if got != "" {
				t.Errorf("url = %q, want empty", got)
			}
			if tt.wantMiss {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			for _, want := range tt.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("errors.Is(%v, %v) = false, want true", err, want)
				}
			}
		})
	}
}
