package details_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/artworkfallback"
	"altune/go-api/internal/search/internal/adapters/providers/coverartarchive"
	"altune/go-api/internal/search/internal/adapters/providers/discogs"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/details"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type toServer struct {
	host string
}

func (t toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = "http", t.host
	return http.DefaultTransport.RoundTrip(req)
}

func serverClient(serverURL string, followRedirects bool) *http.Client {
	client := &http.Client{Transport: toServer{host: strings.TrimPrefix(serverURL, "http://")}}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client
}

type callerMBIDEnricher struct {
	enrichment domain.MusicBrainzEnrichment
}

func (f *callerMBIDEnricher) ResolveMBID(context.Context, search.ResultKind, string, string) (string, error) {
	return "", nil
}

func (f *callerMBIDEnricher) Lookup(context.Context, search.ResultKind, string) (domain.MusicBrainzEnrichment, error) {
	return f.enrichment, nil
}

type memEnrichmentCache struct {
	positive map[string]domain.MusicBrainzEnrichment
	negative map[string]bool
}

func newMemEnrichmentCache() *memEnrichmentCache {
	return &memEnrichmentCache{positive: map[string]domain.MusicBrainzEnrichment{}, negative: map[string]bool{}}
}

func (c *memEnrichmentCache) Get(_ context.Context, kind search.ResultKind, mbid string) (domain.MusicBrainzEnrichment, bool, error) {
	e, ok := c.positive[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *memEnrichmentCache) Set(_ context.Context, kind search.ResultKind, mbid string, e domain.MusicBrainzEnrichment) error {
	c.positive[kind.String()+"|"+mbid] = e
	return nil
}

func (c *memEnrichmentCache) GetNegative(_ context.Context, kind search.ResultKind, nameKey string) (bool, error) {
	return c.negative[kind.String()+"|"+nameKey], nil
}

func (c *memEnrichmentCache) SetNegative(_ context.Context, kind search.ResultKind, nameKey string) error {
	c.negative[kind.String()+"|"+nameKey] = true
	return nil
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainReachesCoverArtArchive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %q, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	caa := coverartarchive.NewCoverArtArchiveResolver(serverClient(srv.URL, false))
	chain := artworkfallback.NewChainedArtworkLookup(caa, coverartarchive.NewCoverArtArchiveIdentityResolver(caa))
	enrichment := domain.EmptyEnrichment()
	enrichment.MBID, enrichment.Genres, enrichment.Year = "mbid-1", []string{"hip hop"}, 2017
	svc := newService(t, &callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache(), details.NoMBIDIndex{})

	got, err := svc.Execute(context.Background(), search.ResultKindAlbum, "Some Other Album", "Other Artist", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "https://coverartarchive.org/release-group/mbid-1/front-1200"; got.ArtworkURL != want {
		t.Errorf("artwork_url = %q, want the chain's mbid-native CoverArtArchive answer %q, not a verified-empty miss", got.ArtworkURL, want)
	}
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainForwardsBridgedExternalIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artists/38" {
			t.Errorf("path = %q, want the exact bridged discogs id, no name search", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 38, "images": [{"type": "primary", "uri": "https://img/real-cover.jpg"}]}`))
	}))
	defer srv.Close()
	chain := artworkfallback.NewChainedArtworkLookup(discogs.NewDiscogsAdapter(serverClient(srv.URL, true), "test-token", "altune-test/1.0"))
	enrichment := domain.EmptyEnrichment()
	enrichment.MBID = "mbid-artist-1"
	enrichment.ExternalIDs = map[domain.ProviderKey]string{"discogs": "38"}
	svc := newService(t, &callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache(), details.NoMBIDIndex{})

	got, err := svc.Execute(context.Background(), search.ResultKindArtist, "Some Other Artist Name", "", "mbid-artist-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://img/real-cover.jpg" {
		t.Errorf("artwork_url = %q, want the bridged Discogs id's cover: a caller-supplied mbid must forward the MB-known ExternalIDs", got.ArtworkURL)
	}
}
