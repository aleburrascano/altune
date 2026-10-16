package ytmsearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func canaryServer(t *testing.T, search, next func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/youtubei/v1/search":
			search(w)
		case "/youtubei/v1/next":
			next(w)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientCanary_NilWhenSearchAndNextAnswer(t *testing.T) {
	srv := canaryServer(t,
		func(w http.ResponseWriter) { _, _ = w.Write(fixture(t, "search_lucki_2021_vibes.json")) },
		func(w http.ResponseWriter) { _, _ = w.Write(fixture(t, "next_u4GAeAYGAqE.json")) })

	if err := clientFor(srv, 0, time.Second).Canary(context.Background()); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}
}

func TestClientCanary_DarkWhenSearchOrNextFails(t *testing.T) {
	okSearch := func(w http.ResponseWriter) { _, _ = w.Write(fixture(t, "search_lucki_2021_vibes.json")) }
	okNext := func(w http.ResponseWriter) { _, _ = w.Write(fixture(t, "next_u4GAeAYGAqE.json")) }
	tests := []struct {
		name   string
		search func(w http.ResponseWriter)
		next   func(w http.ResponseWriter)
		want   string
	}{
		{"search 400", func(w http.ResponseWriter) { http.Error(w, "bad", http.StatusBadRequest) }, okNext, "search:"},
		{"search without ATV song", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) }, okNext, "search:"},
		{"next 400", okSearch, func(w http.ResponseWriter) { http.Error(w, "bad", http.StatusBadRequest) }, "next:"},
		{"next without track", okSearch, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) }, "next:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := clientFor(canaryServer(t, tt.search, tt.next), 0, time.Second).Canary(context.Background())
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("Canary() = %v, want an error starting %q", err, tt.want)
			}
		})
	}
}
