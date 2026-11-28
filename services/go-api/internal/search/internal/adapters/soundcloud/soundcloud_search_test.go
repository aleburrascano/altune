package soundcloud_test

import (
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type failingFallback struct{ called bool }

func (f *failingFallback) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	f.called = true
	return nil, errors.New("fallback down")
}

func TestSoundCloudSearch_FallbackDoesNotLogQueryText(t *testing.T) {
	api := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	buf := contracttest.CaptureDefaultLog(t)

	fb := &failingFallback{}
	a := newTestSoundCloudAPI(t, api, fb)
	_, _ = a.Search(context.Background(), contracttest.SensitiveQuery, contracttest.TrackKinds())

	logged := buf.String()
	if !fb.called || !strings.Contains(logged, "soundcloud.apiv2_fallback") {
		t.Fatalf("expected the api-v2 fallback to be taken and logged, got:\n%s", logged)
	}
	contracttest.AssertNoQueryText(t, logged)
}

func TestSoundCloudAPIAdapter_authRetryReResolveFailureSurfaces(t *testing.T) {
	var homes atomic.Int32
	site := soundCloudSite(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/users") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	a := newSoundCloudAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && homes.Add(1) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		site(w, r)
	}, nil)

	_, err := a.Search(context.Background(), "che", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err == nil || !strings.Contains(err.Error(), "soundcloud home") {
		t.Fatalf("err = %v, want the re-resolve failure surfaced", err)
	}
	if homes.Load() != 2 {
		t.Errorf("home page fetched %d times, want 2 (the 401 drops the client id)", homes.Load())
	}
}

func TestSoundCloudSearch_FailedAlbumsMakeThePartialVisible(t *testing.T) {
	api := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/albums") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"collection":[]}`))
	}

	a := newTestSoundCloudAPI(t, api, nil)
	_, err := a.Search(context.Background(), "humble", contracttest.AllKinds())

	var partial *domain.PartialKindsError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a PartialKindsError for the failed album search", err)
	}
	if len(partial.Failed) != 1 || partial.Failed[0] != domain.ResultKindAlbum {
		t.Errorf("failed kinds = %v, want [album]", partial.Failed)
	}
}
