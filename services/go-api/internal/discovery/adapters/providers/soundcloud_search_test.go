package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingFallback struct{ called bool }

func (f *failingFallback) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	f.called = true
	return nil, errors.New("fallback down")
}

func TestSoundCloudSearch_FallbackDoesNotLogQueryText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	buf := captureDefaultLog(t)

	fb := &failingFallback{}
	a := newTestSoundCloudAPI(srv, fb)
	_, _ = a.Search(context.Background(), sensitiveQuery, trackKinds())

	logged := buf.String()
	if !fb.called || !strings.Contains(logged, "soundcloud.apiv2_fallback") {
		t.Fatalf("expected the api-v2 fallback to be taken and logged, got:\n%s", logged)
	}
	assertNoQueryText(t, logged)
}

func TestSoundCloudAPIAdapter_authRetryReResolveFailureSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/users") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	a.resolver.siteURL = srv.URL

	_, err := a.searchArtists(context.Background(), "che")
	// The seeded client_id makes the first resolve succeed, so a resolve failure here can
	// only come from the post-invalidate re-resolve; withAuthRetry surfaces it verbatim.
	if err == nil || !strings.Contains(err.Error(), "soundcloud home") {
		t.Fatalf("err = %v, want the re-resolve failure surfaced", err)
	}
}
