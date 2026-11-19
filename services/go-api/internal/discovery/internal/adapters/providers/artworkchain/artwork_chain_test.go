package artworkchain_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type slowArtworkResolver struct {
	perCallDelay time.Duration
}

func (s *slowArtworkResolver) Resolve(ctx context.Context, _ discovery.ResultKind, _, _, _ string) (string, error) {
	select {
	case <-time.After(s.perCallDelay):
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestChainedArtworkResolver_AggregateTimeoutCapsFullMiss(t *testing.T) {
	const perCall = 200 * time.Millisecond
	const resolverCount = 10

	resolvers := make([]discovery.ArtworkResolver, resolverCount)
	for i := range resolvers {
		resolvers[i] = &slowArtworkResolver{perCallDelay: perCall}
	}
	const aggregate = 500 * time.Millisecond
	chain := discovery.NewChainedArtworkResolverWithin(aggregate, resolvers...)

	start := time.Now()
	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if !errors.Is(err, discovery.ErrArtworkDegraded) {
		t.Fatalf("a miss cut short by the deadline must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Fatalf("expected empty URL on full miss, got %q", url)
	}

	sumOfPerResolver := perCall * resolverCount
	if elapsed >= sumOfPerResolver {
		t.Fatalf("chain walked the full per-resolver sum (%v >= %v); no aggregate deadline", elapsed, sumOfPerResolver)
	}
	if elapsed > aggregate+perCall {
		t.Fatalf("chain exceeded aggregate bound: elapsed %v, bound ~%v", elapsed, aggregate)
	}
}

func TestChainedArtworkResolver_ShorterCallerDeadlineWins(t *testing.T) {
	resolvers := []discovery.ArtworkResolver{
		&slowArtworkResolver{perCallDelay: time.Second},
		&slowArtworkResolver{perCallDelay: time.Second},
	}
	chain := discovery.NewChainedArtworkResolverWithin(10*time.Second, resolvers...)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _ = chain.ResolveTagged(ctx, discovery.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("shorter caller deadline did not win: elapsed %v", elapsed)
	}
}

func TestChainedArtworkResolver_ReturnsFirstNonEmpty(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/cover.jpg", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/other.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/cover.jpg" {
		t.Errorf("expected second resolver's URL, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsErrors(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: fmt.Errorf("network error")},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/fallback.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/fallback.jpg" {
		t.Errorf("expected fallback URL after error, got %q", url)
	}
}

func TestChainedArtworkResolver_AllEmpty(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when all resolvers return empty, got %q", url)
	}
}

func TestChainedArtworkResolver_MissAfterAFailureIsDegraded(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{Err: fmt.Errorf("coverartarchive 503")},
		&discoverytest.ArtworkResolver{URL: ""},
	)

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindTrack, "Song", "Artist", "mbid")

	if !errors.Is(err, discovery.ErrArtworkDegraded) {
		t.Fatalf("a miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_IdentityMissAfterAFailureIsDegraded(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&fakeIdentityResolver{err: fmt.Errorf("discogs 503")},
		&fakeIdentityResolver{url: ""},
	)

	url, _, err := chain.ResolveWithIdentityTagged(
		context.Background(), discovery.ResultKindAlbum, "DAMN.", "Kendrick Lamar", discovery.ArtworkIdentity{MBID: "mbid"})

	if !errors.Is(err, discovery.ErrArtworkDegraded) {
		t.Fatalf("an identity miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_NoResolvers(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver()

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL with no resolvers, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsDeezerPlaceholder(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/real.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/real.jpg" {
		t.Errorf("expected real URL after skipping Deezer placeholder, got %q", url)
	}
}

type fakeIdentityResolver struct {
	url        string
	err        error
	source     string
	nameCalled bool
	idCalled   bool
}

func (f *fakeIdentityResolver) Resolve(context.Context, discovery.ResultKind, string, string, string) (string, error) {
	f.nameCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ResolveByIdentity(context.Context, discovery.ResultKind, discovery.ArtworkIdentity) (string, error) {
	f.idCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ArtworkSource() discovery.ProviderKey {
	return discovery.ProviderKey(f.source)
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged(t *testing.T) {
	nameOnly := &discoverytest.ArtworkResolver{URL: "https://img/name-guess.jpg"}
	failing := &fakeIdentityResolver{err: errors.New("boom"), source: "broken"}
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}

	chain := discovery.NewChainedArtworkResolver(nameOnly, failing, identity)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), discovery.ResultKindArtist, "Che", "",
		discovery.ArtworkIdentity{ExternalIDs: map[discovery.ProviderKey]string{"discogs": "38"}},
	)
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://img/identity.jpg" {
		t.Errorf("url = %q, want the identity resolver's image (errors skipped, name resolvers never tried)", url)
	}
	if source != "discogs" {
		t.Errorf("source = %q, want the resolver's tag", source)
	}
	if !failing.idCalled || !identity.idCalled {
		t.Error("both identity resolvers must be tried in order")
	}
	if failing.nameCalled || identity.nameCalled {
		t.Error("the identity path must never fall back to a name search — that is the caller's labelled-provisional decision")
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_skipsPlaceholder(t *testing.T) {
	placeholder := &fakeIdentityResolver{url: discovery.DeezerPlaceholderImage, source: "deezer"}
	real := &fakeIdentityResolver{url: "https://img/real.jpg", source: "discogs"}

	chain := discovery.NewChainedArtworkResolver(placeholder, real)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), discovery.ResultKindArtist, "Che", "", discovery.ArtworkIdentity{})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://img/real.jpg" || source != "discogs" {
		t.Errorf("(%q, %q), want the placeholder skipped", url, source)
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_noIdentitySourceIsEmpty(t *testing.T) {
	chain := discovery.NewChainedArtworkResolver(&discoverytest.ArtworkResolver{URL: "https://img/name.jpg"})
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), discovery.ResultKindArtist, "Che", "", discovery.ArtworkIdentity{})
	if err != nil || url != "" || source != "" {
		t.Errorf("(%q, %q, %v), want empty — no identity-capable resolver in the chain", url, source, err)
	}
}

func TestChainedArtworkResolver_ResolveTagged_skipsIdentityResolvers(t *testing.T) {
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}
	name := &discoverytest.ArtworkResolver{URL: "https://img/name.jpg"}

	chain := discovery.NewChainedArtworkResolver(identity, name)
	url, source, err := chain.ResolveTagged(context.Background(), discovery.ResultKindArtist, "Che", "", "")
	if err != nil {
		t.Fatalf("ResolveTagged: %v", err)
	}
	if url != "https://img/name.jpg" {
		t.Errorf("url = %q, want the name resolver's image", url)
	}
	if identity.nameCalled || identity.idCalled {
		t.Error("identity-only resolver must be skipped entirely on the name path")
	}
	if source != "" {
		t.Errorf("source = %q, want empty for an unsourced resolver", source)
	}
}

func TestChainedArtworkResolver_MissFromAFailingResolverIsDegradedAndUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	coverArtArchiveOn500 := discovery.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL))

	chain := discovery.NewChainedArtworkResolver(coverArtArchiveOn500)
	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" {
		t.Errorf("url = %q, want empty on a failing resolver", url)
	}
	if !errors.Is(err, discovery.ErrArtworkDegraded) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkDegraded)", err)
	}
	if !errors.Is(err, discovery.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkUnavailable)", err)
	}
}

func TestChainedArtworkResolver_CoverArtArchive404IsAVerifiedMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	chain := discovery.NewChainedArtworkResolver(discovery.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL)))
	url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" || err != nil {
		t.Errorf("ResolveTagged on a CAA 404 = (%q, %v), want (\"\", nil)", url, err)
	}
	if discovery.IsUnverifiedArtworkMiss(url, err) {
		t.Error("IsUnverifiedArtworkMiss = true, want false so the verified miss is negative-cached")
	}
}

func TestChainedArtworkResolver_CoverArtArchiveAnswersIdentityWalkOnMBIDAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := discovery.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := discovery.NewChainedArtworkResolver(caa, discovery.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveWithIdentityTagged(context.Background(), discovery.ResultKindAlbum,
		"An unrelated title", "An unrelated subtitle", discovery.ArtworkIdentity{MBID: "rg-1"})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive's mbid-native answer reached from the identity walk", url)
	}
	if source != discovery.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, discovery.ProviderKeyCoverArtArchive)
	}
}

func TestChainedArtworkResolver_CoverArtArchiveStillAnswersResolveTaggedAlongsideItsIdentityWrapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := discovery.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := discovery.NewChainedArtworkResolver(caa, discovery.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveTagged(context.Background(), discovery.ResultKindAlbum, "Album", "Artist", "rg-1")
	if err != nil {
		t.Fatalf("ResolveTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive still reachable from the name walk", url)
	}
	if source != discovery.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, discovery.ProviderKeyCoverArtArchive)
	}
}

func failingArtworkProviders(srv *httptest.Server) map[string]discovery.ArtworkResolver {
	return map[string]discovery.ArtworkResolver{
		"genius":     discovery.NewGeniusArtworkResolver(providertest.NewClient(srv.URL), "token"),
		"ytmusic":    discovery.NewYouTubeMusicArtworkResolver(&providertest.RedirectTransport{TargetURL: srv.URL}),
		"theaudiodb": discovery.NewTheAudioDBAdapter(providertest.NewClient(srv.URL), discovery.WithTheAudioDBAPIKey("k")),
	}
}

func TestChainedArtworkResolver_NextProviderStillReturnsArtworkAfterAProviderFails(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer failing.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists":[{"idArtist":"1","strArtist":"sombr","strArtistThumb":"https://img.example/sombr.jpg"}]}`))
	}))
	defer healthy.Close()

	for name, broken := range failingArtworkProviders(failing) {
		t.Run(name, func(t *testing.T) {
			chain := discovery.NewChainedArtworkResolver(broken, discovery.NewTheAudioDBAdapter(providertest.NewClient(healthy.URL), discovery.WithTheAudioDBAPIKey("k")))
			url, source, err := chain.ResolveTagged(context.Background(), discovery.ResultKindArtist, "sombr", "", "")
			if err != nil {
				t.Fatalf("ResolveTagged after %s fails = (%q, %q, %v), want no error", name, url, source, err)
			}
			if url != "https://img.example/sombr.jpg" || source != "theaudiodb" {
				t.Errorf("ResolveTagged after %s fails = (%q, %q), want (%q, %q)", name, url, source, "https://img.example/sombr.jpg", "theaudiodb")
			}
		})
	}
}

func TestChainedArtworkResolver_MissAfterAProviderFailsIsDegradedAndUnavailable(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failing.Close()

	for name, broken := range failingArtworkProviders(failing) {
		t.Run(name, func(t *testing.T) {
			chain := discovery.NewChainedArtworkResolver(broken, &discoverytest.ArtworkResolver{})
			url, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindArtist, "sombr", "", "")
			if url != "" || !errors.Is(err, discovery.ErrArtworkDegraded) || !errors.Is(err, discovery.ErrArtworkUnavailable) {
				t.Errorf("ResolveTagged after %s fails then a miss = (%q, %v), want ErrArtworkDegraded and ErrArtworkUnavailable", name, url, err)
			}
			if !discovery.IsUnverifiedArtworkMiss(url, err) {
				t.Error("IsUnverifiedArtworkMiss = false, want true so the miss is not negative-cached")
			}
		})
	}
}

func TestChainedArtworkResolver_DeezerOutageThenITunesMissIsDegraded(t *testing.T) {
	deezerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer deezerSrv.Close()
	itunesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"resultCount":0,"results":[]}`))
	}))
	defer itunesSrv.Close()

	chain := discovery.NewChainedArtworkResolver(
		discovery.NewDeezerAdapter(providertest.NewClient(deezerSrv.URL)),
		discovery.NewITunesAdapter(providertest.NewClient(itunesSrv.URL)),
	)
	art, _, err := chain.ResolveTagged(context.Background(), discovery.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, discovery.ErrArtworkDegraded) {
		t.Errorf("ResolveTagged with Deezer down and no iTunes match = (%q, %v), want (\"\", ErrArtworkDegraded)", art, err)
	}
}

type callerMBIDEnricher struct {
	enrichment discovery.MBEnrichment
}

func (f *callerMBIDEnricher) ResolveMBID(context.Context, discovery.ResultKind, string, string) (string, error) {
	return "", nil
}

func (f *callerMBIDEnricher) Lookup(context.Context, discovery.ResultKind, string) (discovery.MBEnrichment, error) {
	return f.enrichment, nil
}

type memEnrichmentCache struct {
	positive map[string]discovery.MBEnrichment
	negative map[string]bool
}

func newMemEnrichmentCache() *memEnrichmentCache {
	return &memEnrichmentCache{positive: map[string]discovery.MBEnrichment{}, negative: map[string]bool{}}
}

func (c *memEnrichmentCache) Get(_ context.Context, kind discovery.ResultKind, mbid string) (discovery.MBEnrichment, bool, error) {
	e, ok := c.positive[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *memEnrichmentCache) Set(_ context.Context, kind discovery.ResultKind, mbid string, e discovery.MBEnrichment) error {
	c.positive[kind.String()+"|"+mbid] = e
	return nil
}

func (c *memEnrichmentCache) GetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) (bool, error) {
	return c.negative[kind.String()+"|"+nameKey], nil
}

func (c *memEnrichmentCache) SetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) error {
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
	caa := discovery.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := discovery.NewChainedArtworkResolver(caa, discovery.NewCoverArtArchiveIdentityResolver(caa))
	enrichment := discovery.EmptyEnrichment()
	enrichment.MBID, enrichment.Genres, enrichment.Year = "mbid-1", []string{"hip hop"}, 2017
	svc := discovery.NewEnrichmentService(&callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache())

	got, err := svc.Execute(context.Background(), discovery.ResultKindAlbum, "Some Other Album", "Other Artist", "mbid-1")
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
	chain := discovery.NewChainedArtworkResolver(discovery.NewDiscogsAdapter(providertest.NewClient(srv.URL), "test-token", "altune-test/1.0"))
	enrichment := discovery.EmptyEnrichment()
	enrichment.MBID = "mbid-artist-1"
	enrichment.ExternalIDs = map[discovery.ProviderKey]string{"discogs": "38"}
	svc := discovery.NewEnrichmentService(&callerMBIDEnricher{enrichment: enrichment}, chain, newMemEnrichmentCache())

	got, err := svc.Execute(context.Background(), discovery.ResultKindArtist, "Some Other Artist Name", "", "mbid-artist-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://img/real-cover.jpg" {
		t.Errorf("artwork_url = %q, want the bridged Discogs id's cover: a caller-supplied mbid must forward the MB-known ExternalIDs", got.ArtworkURL)
	}
}
