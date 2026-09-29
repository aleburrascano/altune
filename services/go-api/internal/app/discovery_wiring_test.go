package app

import (
	"altune/go-api/internal/auth"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	"altune/go-api/internal/discovery/adapters/providers"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type countingProviderRT struct {
	mu    sync.Mutex
	calls int
}

func (rt *countingProviderRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func (rt *countingProviderRT) roundTrips() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.calls
}

func totalProviderCounts(s providermetrics.Snapshot) int64 {
	var total int64
	for _, o := range s {
		total += o.OK + o.Quota + o.Error
	}
	return total
}

func TestRequestPathProviderCallsAreCountedOnce(t *testing.T) {
	routes := []struct {
		name   string
		target string
	}{
		{"album content", "/albums/deezer/1/tracks"},
		{"lyrics", "/lyrics?title=song&subtitle=artist"},
		{"enrichment", "/enrichment?kind=album&title=album"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			rt := &countingProviderRT{}
			a := &App{cfg: &config.Config{MusicBrainzUserAgent: "altune-test/1.0"}}
			disc := a.wireDiscovery(context.Background(), newClientFactory(countedProviderTransport(rt)))
			before := providermetrics.ReadSnapshot()
			req := httptest.NewRequest(http.MethodGet, route.target, nil)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
			disc.handler.Routes().ServeHTTP(httptest.NewRecorder(), req)
			counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)

			if rt.roundTrips() == 0 {
				t.Fatalf("%s made no provider call, so it proves nothing about the transport", route.target)
			}
			if counted != int64(rt.roundTrips()) {
				t.Errorf("provider counters moved by %d over %d provider calls, want one count per call", counted, rt.roundTrips())
			}
		})
	}
}

type enrichRealChainFakeEnricher struct {
	enrichment discoveryDomain.MBEnrichment
}

func (f *enrichRealChainFakeEnricher) ResolveMBID(_ context.Context, _ discoveryDomain.ResultKind, _, _ string) (string, error) {
	return "", nil
}

func (f *enrichRealChainFakeEnricher) Lookup(_ context.Context, _ discoveryDomain.ResultKind, _ string) (discoveryDomain.MBEnrichment, error) {
	return f.enrichment, nil
}

type enrichRealChainMemCache struct {
	pos map[string]discoveryDomain.MBEnrichment
	neg map[string]bool
}

func newEnrichRealChainMemCache() *enrichRealChainMemCache {
	return &enrichRealChainMemCache{pos: map[string]discoveryDomain.MBEnrichment{}, neg: map[string]bool{}}
}

func (c *enrichRealChainMemCache) Get(_ context.Context, kind discoveryDomain.ResultKind, mbid string) (discoveryDomain.MBEnrichment, bool, error) {
	e, ok := c.pos[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *enrichRealChainMemCache) Set(_ context.Context, kind discoveryDomain.ResultKind, mbid string, e discoveryDomain.MBEnrichment) error {
	c.pos[kind.String()+"|"+mbid] = e
	return nil
}

func (c *enrichRealChainMemCache) GetNegative(_ context.Context, kind discoveryDomain.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+"|"+nameKey], nil
}

func (c *enrichRealChainMemCache) SetNegative(_ context.Context, kind discoveryDomain.ResultKind, nameKey string) error {
	c.neg[kind.String()+"|"+nameKey] = true
	return nil
}

func enrichRealChainSampleEnrichment() discoveryDomain.MBEnrichment {
	e := discoveryDomain.EmptyEnrichment()
	e.MBID = "mbid-1"
	e.Genres = []string{"hip hop"}
	e.Year = 2017
	return e
}

type enrichTestHostRewriteTransport struct {
	targetURL string
}

func (t *enrichTestHostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.targetURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func rewrittenClient(targetURL string) *http.Client {
	return &http.Client{Transport: &enrichTestHostRewriteTransport{targetURL: targetURL}}
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainReachesCoverArtArchive(t *testing.T) {
	caaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %q, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer caaServer.Close()

	caa := providers.NewCoverArtArchiveResolver(rewrittenClient(caaServer.URL))
	chain := providers.NewChainedArtworkResolver(caa, providers.NewCoverArtArchiveIdentityResolver(caa))

	enr := &enrichRealChainFakeEnricher{enrichment: enrichRealChainSampleEnrichment()}
	svc := discoveryEnrich.NewEnrichmentService(enr, chain, newEnrichRealChainMemCache())

	got, err := svc.Execute(context.Background(), discoveryDomain.ResultKindAlbum,
		"Some Other Album", "Other Artist", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "https://coverartarchive.org/release-group/mbid-1/front-1200"
	if got.ArtworkURL != want {
		t.Errorf("artwork_url = %q, want the real chain's mbid-native CoverArtArchive answer %q — "+
			"the caller-supplied-mbid path must still reach a title-blind provider instead of a "+
			"verified-empty miss that would poison the shared cache with a blank cover", got.ArtworkURL, want)
	}
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainForwardsBridgedExternalIDs(t *testing.T) {
	discogsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artists/38" {
			t.Errorf("path = %q, want the exact bridged discogs id, no name search", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 38, "images": [{"type": "primary", "uri": "https://img/real-cover.jpg"}]}`))
	}))
	defer discogsServer.Close()

	discogs := providers.NewDiscogsAdapter(rewrittenClient(discogsServer.URL), "test-token", "altune-test/1.0")
	chain := providers.NewChainedArtworkResolver(discogs)

	e := discoveryDomain.EmptyEnrichment()
	e.MBID = "mbid-artist-1"
	e.ExternalIDs = map[string]string{"discogs": "38"}
	enr := &enrichRealChainFakeEnricher{enrichment: e}
	svc := discoveryEnrich.NewEnrichmentService(enr, chain, newEnrichRealChainMemCache())

	got, err := svc.Execute(context.Background(), discoveryDomain.ResultKindArtist,
		"Some Other Artist Name", "", "mbid-artist-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://img/real-cover.jpg" {
		t.Errorf("artwork_url = %q, want the bridged Discogs id's real cover — "+
			"the caller-supplied-mbid path must forward the MB-known ExternalIDs, not resolve on mbid alone",
			got.ArtworkURL)
	}
}

type failingBehavioralStore struct{ err error }

func (s failingBehavioralStore) SatisfactionSignals(context.Context, time.Time) ([]discoveryPorts.BehavioralSignal, error) {
	return nil, s.err
}

func TestBehavioralRankingRefresh_LogsOnFailure(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(restore)

	boom := errors.New("boom")
	consumer := discoveryService.NewSatisfactionConsumer(failingBehavioralStore{err: boom})
	searchSvc := discoveryService.NewService(nil, nil, discoveryService.WithBehavioralRanking(consumer))

	a := &App{cfg: &config.Config{BehavioralRankingEnabled: true}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	a.startDiscoveryBackgroundJobs(ctx, newClientFactory(nil), searchSvc, nil, nil)

	h := waitForHealth(t, a, jobBehavioralRankingRefresh, func(h JobHealth) bool { return h.Failures >= 1 })

	if h.LastFailure.IsZero() {
		t.Error("a failed run must still update LastFailure through jc.record")
	}

	logged := buf.String()
	wantFailed := `level=WARN msg="behavioral ranking refresh failed" error=boom` + "\n"
	if !strings.Contains(logged, wantFailed) {
		t.Errorf("failure line drifted: want %q in\n%s", wantFailed, logged)
	}
}
