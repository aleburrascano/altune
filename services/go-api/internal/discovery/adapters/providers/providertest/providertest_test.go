package providertest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_RewritesHostKeepsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	defer srv.Close()

	resp, err := NewClient(srv.URL).Get("https://theaudiodb.com/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if gotPath != "/x" {
		t.Errorf("server saw path %q, want /x", gotPath)
	}
}

func TestArtworkFailureHandlers_AllFail(t *testing.T) {
	handlers := ArtworkFailureHandlers()
	if len(handlers) == 0 {
		t.Fatal("no failure handlers")
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Get(srv.URL + "/p")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			b, _ := io.ReadAll(resp.Body)
			if resp.Header.Get("Content-Type") == "text/html" {
				return
			}
			if len(b) > 0 && b[0] == '{' && b[len(b)-1] == '}' {
				t.Errorf("200 with well-formed object body %q", b)
			}
		})
	}
}

func TestCancelMidRequestServer_CancelsContextOnRequest(t *testing.T) {
	srv, ctx := CancelMidRequestServer(t)
	if ctx.Err() != nil {
		t.Fatal("context cancelled before any request")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the request to fail once cancelled")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after request reached the server")
	}
}
