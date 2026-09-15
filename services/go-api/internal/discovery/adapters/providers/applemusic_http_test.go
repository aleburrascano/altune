package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// appleMusicCallSites exercises every Apple Music catalog GET so the request
// contract (headers, status handling, decode-error wrapping) is pinned for all
// of them at once.
func appleMusicCallSites() []struct {
	name       string
	call       func(ctx context.Context, a *AppleMusicAdapter) error
	decodeText string
} {
	all := map[domain.ResultKind]bool{domain.ResultKindTrack: true, domain.ResultKindAlbum: true, domain.ResultKindArtist: true}
	return []struct {
		name       string
		call       func(ctx context.Context, a *AppleMusicAdapter) error
		decodeText string
	}{
		{"search", func(ctx context.Context, a *AppleMusicAdapter) error {
			_, err := a.Search(ctx, "q", all)
			return err
		}, "decode catalog search response: "},
		{"album tracks", func(ctx context.Context, a *AppleMusicAdapter) error {
			_, err := a.GetAlbumTracks(ctx, domain.ProviderAppleMusic, "al-1")
			return err
		}, "decode album tracks response: "},
		{"artist albums", func(ctx context.Context, a *AppleMusicAdapter) error {
			_, err := a.GetArtistAlbums(ctx, domain.ProviderAppleMusic, "ar-1")
			return err
		}, "decode catalog response: "},
		{"artist top tracks", func(ctx context.Context, a *AppleMusicAdapter) error {
			_, err := a.GetArtistTopTracks(ctx, domain.ProviderAppleMusic, "ar-1")
			return err
		}, "decode catalog response: "},
	}
}

func newAppleMusicCallSiteAdapter(srv *httptest.Server) *AppleMusicAdapter {
	a := newTestAppleMusicAdapter(srv)
	a.catalogBase = srv.URL
	return a
}

func TestAppleMusicAdapter_callSites_sendRequestHeaders(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			if err := tc.call(t.Context(), newAppleMusicCallSiteAdapter(srv)); err != nil {
				t.Fatalf("call: %v", err)
			}
			want := map[string]string{
				"Authorization": "Bearer test-token",
				"Origin":        appleMusicOrigin,
				"User-Agent":    appleMusicUserAgent,
			}
			for k, v := range want {
				if got.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestAppleMusicAdapter_callSites_non200IsStatusError(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			err := tc.call(t.Context(), newAppleMusicCallSiteAdapter(srv))
			if err == nil || err.Error() != "http status 500" {
				t.Fatalf("err = %v, want exactly %q", err, "http status 500")
			}
		})
	}
}

func TestAppleMusicAdapter_callSites_malformedBodyIsWrappedDecodeError(t *testing.T) {
	for _, tc := range appleMusicCallSites() {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`<html>maintenance</html>`))
			}))
			defer srv.Close()

			err := tc.call(t.Context(), newAppleMusicCallSiteAdapter(srv))
			if err == nil || !strings.HasPrefix(err.Error(), tc.decodeText) {
				t.Fatalf("err = %v, want prefix %q", err, tc.decodeText)
			}
		})
	}
}
