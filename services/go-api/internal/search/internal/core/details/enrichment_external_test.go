package details_test

import (
	"altune/go-api/internal/search"
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
	enrichment search.MusicBrainzEnrichment
}

func (f *callerMBIDEnricher) ResolveMBID(context.Context, search.ResultKind, string, string) (string, error) {
	return "", nil
}

func (f *callerMBIDEnricher) Lookup(context.Context, search.ResultKind, string) (search.MusicBrainzEnrichment, error) {
	return f.enrichment, nil
}

type memEnrichmentCache struct {
	positive map[string]search.MusicBrainzEnrichment
	negative map[string]bool
}

func newMemEnrichmentCache() *memEnrichmentCache {
	return &memEnrichmentCache{positive: map[string]search.MusicBrainzEnrichment{}, negative: map[string]bool{}}
}

func (c *memEnrichmentCache) Get(_ context.Context, kind search.ResultKind, mbid string) (search.MusicBrainzEnrichment, bool, error) {
	e, ok := c.positive[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *memEnrichmentCache) Set(_ context.Context, kind search.ResultKind, mbid string, e search.MusicBrainzEnrichment) error {
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
	caa := search.NewCoverArtArchiveResolver(serverClient(srv.URL, false))
	chain := search.NewChainedArtworkLookup(caa, search.NewCoverArtArchiveIdentityResolver(caa))
	enrichment := search.EmptyEnrichment()
	enrichment.MBID, enrichment.Genres, enrichment.Year = "mbid-1", []string{"hip hop"}, 2017
	svc := newService(t, &callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache(), search.NoMBIDIndex{})

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
	chain := search.NewChainedArtworkLookup(search.NewDiscogsAdapter(serverClient(srv.URL, true), "test-token", "altune-test/1.0"))
	enrichment := search.EmptyEnrichment()
	enrichment.MBID = "mbid-artist-1"
	enrichment.ExternalIDs = map[search.ProviderKey]string{"discogs": "38"}
	svc := newService(t, &callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache(), search.NoMBIDIndex{})

	got, err := svc.Execute(context.Background(), search.ResultKindArtist, "Some Other Artist Name", "", "mbid-artist-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://img/real-cover.jpg" {
		t.Errorf("artwork_url = %q, want the bridged Discogs id's cover: a caller-supplied mbid must forward the MB-known ExternalIDs", got.ArtworkURL)
	}
}
