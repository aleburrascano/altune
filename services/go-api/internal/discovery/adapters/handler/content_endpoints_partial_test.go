package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// stubIdentityStore resolves every artist to the same cross-provider bridge,
// so the artist content endpoints take the identity fan-out path.
type stubIdentityStore struct{ xref map[string]string }

func (s stubIdentityStore) PersistBridges(context.Context, discdomain.ResultKind, string, map[string]string) error {
	return nil
}

func (s stubIdentityStore) LookupByProviderID(context.Context, discdomain.ResultKind, discdomain.ProviderKey, string) (string, map[string]string, bool) {
	return "mbid-artist", s.xref, true
}

func (s stubIdentityStore) Invalidate(context.Context, discdomain.ResultKind, discdomain.ProviderKey, string) error {
	return nil
}

// partialContentProvider answers as provider, or fails when down.
type partialContentProvider struct {
	provider discdomain.ProviderName
	down     bool
}

func (p partialContentProvider) result(kind discdomain.ResultKind, id string) ([]discdomain.SearchResult, error) {
	if p.down {
		return nil, errors.New("upstream unavailable")
	}
	return []discdomain.SearchResult{{
		Kind:     kind,
		Title:    "Real Song",
		Subtitle: "Artist",
		Sources:  []discdomain.SourceRef{{Provider: p.provider, ExternalID: id}},
		Extras:   map[string]any{},
	}}, nil
}

func (p partialContentProvider) GetArtistTopTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.result(discdomain.ResultKindTrack, id)
}

func (p partialContentProvider) GetArtistAlbums(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.result(discdomain.ResultKindAlbum, id)
}

// identityContentRouter serves the discovery routes over an identity fan-out
// across every provider, with the ones in down failing.
func identityContentRouter(down map[discdomain.ProviderName]bool) chi.Router {
	all := []discdomain.ProviderName{
		discdomain.ProviderDeezer, discdomain.ProviderMusicBrainz, discdomain.ProviderSoundCloud,
		discdomain.ProviderLastFM, discdomain.ProviderITunes, discdomain.ProviderTheAudioDB,
		discdomain.ProviderDiscogs, discdomain.ProviderYouTube, discdomain.ProviderAmazonMusic,
		discdomain.ProviderAppleMusic, discdomain.ProviderSpotify,
	}
	providers := make(map[discdomain.ProviderName]ports.ArtistContentProvider, len(all))
	xref := make(map[string]string, len(all))
	for _, pn := range all {
		providers[pn] = partialContentProvider{provider: pn, down: down[pn]}
		xref[pn.String()] = "id-" + pn.String()
	}
	svc := service.NewGetArtistContentService(providers,
		service.WithContentIdentityStore(stubIdentityStore{xref: xref}))
	h := NewDiscoveryHandler(DiscoveryServices{Artist: svc})
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())
	return router
}

func allButDeezerDown() map[discdomain.ProviderName]bool {
	return map[discdomain.ProviderName]bool{
		discdomain.ProviderMusicBrainz: true, discdomain.ProviderSoundCloud: true,
		discdomain.ProviderLastFM: true, discdomain.ProviderITunes: true, discdomain.ProviderTheAudioDB: true,
		discdomain.ProviderDiscogs: true, discdomain.ProviderYouTube: true, discdomain.ProviderAmazonMusic: true,
		discdomain.ProviderAppleMusic: true, discdomain.ProviderSpotify: true,
	}
}

// decodeContentWire decodes a content fetch body as raw JSON, so a missing
// "partial" key is distinguishable from false.
func decodeContentWire(t *testing.T, raw json.RawMessage) (status string, items int, partial *bool) {
	t.Helper()
	var body struct {
		Status  string            `json:"status"`
		Items   []json.RawMessage `json:"items"`
		Partial *bool             `json:"partial"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return body.Status, len(body.Items), body.Partial
}

func TestArtistContentEndpoints_IdentityFanOutReportsPartial(t *testing.T) {
	cases := []struct {
		name        string
		down        map[discdomain.ProviderName]bool
		wantPartial bool
	}{
		{name: "10 of 11 providers failed", down: allButDeezerDown(), wantPartial: true},
		{name: "every provider answered", down: nil, wantPartial: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := identityContentRouter(tc.down)
			bodies := map[string]json.RawMessage{}
			for _, path := range []string{"top-tracks", "albums"} {
				rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/id-deezer/"+path+"?name=Artist", nil)
				discAssertStatus(t, rec, http.StatusOK)
				bodies[path] = rec.Body.Bytes()
			}
			rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/id-deezer/content?name=Artist", nil)
			discAssertStatus(t, rec, http.StatusOK)
			var combined map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &combined); err != nil {
				t.Fatalf("decode combined: %v", err)
			}
			bodies["content.top_tracks"] = combined["top_tracks"]
			bodies["content.albums"] = combined["albums"]

			for label, raw := range bodies {
				status, items, partial := decodeContentWire(t, raw)
				if status != "ok" || items == 0 {
					t.Errorf("%s: status = %q, items = %d, want ok with items", label, status, items)
				}
				if partial == nil || *partial != tc.wantPartial {
					t.Errorf("%s: partial = %v, want %v (body %s)", label, partial, tc.wantPartial, raw)
				}
			}
		})
	}
}
