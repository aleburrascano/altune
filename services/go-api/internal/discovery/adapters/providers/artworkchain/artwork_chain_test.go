package artworkchain

import (
	"altune/go-api/internal/discovery/adapters/providers/coverartarchive"
	"altune/go-api/internal/discovery/adapters/providers/deezer"
	"altune/go-api/internal/discovery/adapters/providers/genius"
	"altune/go-api/internal/discovery/adapters/providers/itunes"
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/adapters/providers/theaudiodb"
	"altune/go-api/internal/discovery/adapters/providers/ytmusic"
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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

func (s *slowArtworkResolver) Resolve(ctx context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
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

	resolvers := make([]ports.ArtworkResolver, resolverCount)
	for i := range resolvers {
		resolvers[i] = &slowArtworkResolver{perCallDelay: perCall}
	}
	chain := NewChainedArtworkResolver(resolvers...)
	chain.timeout = 500 * time.Millisecond

	start := time.Now()
	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("a miss cut short by the deadline must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Fatalf("expected empty URL on full miss, got %q", url)
	}

	sumOfPerResolver := perCall * resolverCount
	if elapsed >= sumOfPerResolver {
		t.Fatalf("chain walked the full per-resolver sum (%v >= %v); no aggregate deadline", elapsed, sumOfPerResolver)
	}
	if elapsed > chain.timeout+perCall {
		t.Fatalf("chain exceeded aggregate bound: elapsed %v, bound ~%v", elapsed, chain.timeout)
	}
}

func TestChainedArtworkResolver_ShorterCallerDeadlineWins(t *testing.T) {
	resolvers := []ports.ArtworkResolver{
		&slowArtworkResolver{perCallDelay: time.Second},
		&slowArtworkResolver{perCallDelay: time.Second},
	}
	chain := NewChainedArtworkResolver(resolvers...)
	chain.timeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _ = chain.ResolveTagged(ctx, domain.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("shorter caller deadline did not win: elapsed %v", elapsed)
	}
}

func TestChainedArtworkResolver_ReturnsFirstNonEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/cover.jpg", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/other.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/cover.jpg" {
		t.Errorf("expected second resolver's URL, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsErrors(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: fmt.Errorf("network error")},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/fallback.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/fallback.jpg" {
		t.Errorf("expected fallback URL after error, got %q", url)
	}
}

func TestChainedArtworkResolver_AllEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
		&discoverytest.ArtworkResolver{URL: "", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when all resolvers return empty, got %q", url)
	}
}

func TestChainedArtworkResolver_MissAfterAFailureIsDegraded(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{Err: fmt.Errorf("coverartarchive 503")},
		&discoverytest.ArtworkResolver{URL: ""},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("a miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_IdentityMissAfterAFailureIsDegraded(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeIdentityResolver{err: fmt.Errorf("discogs 503")},
		&fakeIdentityResolver{url: ""},
	)

	url, _, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", ports.ArtworkIdentity{MBID: "mbid"})

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("an identity miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_NoResolvers(t *testing.T) {
	chain := NewChainedArtworkResolver()

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL with no resolvers, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsDeezerPlaceholder(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&discoverytest.ArtworkResolver{URL: "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg", Err: nil},
		&discoverytest.ArtworkResolver{URL: "https://images.example.com/real.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Artist", "", "mbid")
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

func (f *fakeIdentityResolver) Resolve(context.Context, domain.ResultKind, string, string, string) (string, error) {
	f.nameCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ResolveByIdentity(context.Context, domain.ResultKind, ports.ArtworkIdentity) (string, error) {
	f.idCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ArtworkSource() domain.ProviderKey {
	return domain.ProviderKey(f.source)
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged(t *testing.T) {
	nameOnly := &discoverytest.ArtworkResolver{URL: "https://img/name-guess.jpg"}
	failing := &fakeIdentityResolver{err: errors.New("boom"), source: "broken"}
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}

	chain := NewChainedArtworkResolver(nameOnly, failing, identity)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "",
		ports.ArtworkIdentity{ExternalIDs: map[domain.ProviderKey]string{"discogs": "38"}},
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
	placeholder := &fakeIdentityResolver{url: deezer.DeezerPlaceholderImage, source: "deezer"}
	real := &fakeIdentityResolver{url: "https://img/real.jpg", source: "discogs"}

	chain := NewChainedArtworkResolver(placeholder, real)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "", ports.ArtworkIdentity{})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://img/real.jpg" || source != "discogs" {
		t.Errorf("(%q, %q), want the placeholder skipped", url, source)
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_noIdentitySourceIsEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(&discoverytest.ArtworkResolver{URL: "https://img/name.jpg"})
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "", ports.ArtworkIdentity{})
	if err != nil || url != "" || source != "" {
		t.Errorf("(%q, %q, %v), want empty — no identity-capable resolver in the chain", url, source, err)
	}
}

func TestChainedArtworkResolver_ResolveTagged_skipsIdentityResolvers(t *testing.T) {
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}
	name := &discoverytest.ArtworkResolver{URL: "https://img/name.jpg"}

	chain := NewChainedArtworkResolver(identity, name)
	url, source, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Che", "", "")
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

	coverArtArchiveOn500 := coverartarchive.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL))

	chain := NewChainedArtworkResolver(coverArtArchiveOn500)
	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" {
		t.Errorf("url = %q, want empty on a failing resolver", url)
	}
	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkDegraded)", err)
	}
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkUnavailable)", err)
	}
}

func TestChainedArtworkResolver_CoverArtArchive404IsAVerifiedMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	chain := NewChainedArtworkResolver(coverartarchive.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL)))
	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" || err != nil {
		t.Errorf("ResolveTagged on a CAA 404 = (%q, %v), want (\"\", nil)", url, err)
	}
	if ports.IsUnverifiedArtworkMiss(url, err) {
		t.Error("IsUnverifiedArtworkMiss = true, want false so the verified miss is negative-cached")
	}
}

func TestChainedArtworkResolver_CoverArtArchiveAnswersIdentityWalkOnMBIDAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := coverartarchive.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := NewChainedArtworkResolver(caa, coverartarchive.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveWithIdentityTagged(context.Background(), domain.ResultKindAlbum,
		"An unrelated title", "An unrelated subtitle", ports.ArtworkIdentity{MBID: "rg-1"})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive's mbid-native answer reached from the identity walk", url)
	}
	if source != domain.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, domain.ProviderKeyCoverArtArchive)
	}
}

func TestChainedArtworkResolver_CoverArtArchiveStillAnswersResolveTaggedAlongsideItsIdentityWrapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := coverartarchive.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := NewChainedArtworkResolver(caa, coverartarchive.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveTagged(context.Background(), domain.ResultKindAlbum, "Album", "Artist", "rg-1")
	if err != nil {
		t.Fatalf("ResolveTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive still reachable from the name walk", url)
	}
	if source != domain.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, domain.ProviderKeyCoverArtArchive)
	}
}

func failingArtworkProviders(srv *httptest.Server) map[string]ports.ArtworkResolver {
	return map[string]ports.ArtworkResolver{
		"genius":     genius.NewGeniusArtworkResolver(providertest.NewClient(srv.URL), "token"),
		"ytmusic":    ytmusic.NewYouTubeMusicArtworkResolver(&providertest.RedirectTransport{TargetURL: srv.URL}),
		"theaudiodb": theaudiodb.New(providertest.NewClient(srv.URL), theaudiodb.WithAPIKey("k")),
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
			chain := NewChainedArtworkResolver(broken, theaudiodb.New(providertest.NewClient(healthy.URL), theaudiodb.WithAPIKey("k")))
			url, source, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "sombr", "", "")
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
			chain := NewChainedArtworkResolver(broken, &discoverytest.ArtworkResolver{})
			url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "sombr", "", "")
			if url != "" || !errors.Is(err, ports.ErrArtworkDegraded) || !errors.Is(err, ports.ErrArtworkUnavailable) {
				t.Errorf("ResolveTagged after %s fails then a miss = (%q, %v), want ErrArtworkDegraded and ErrArtworkUnavailable", name, url, err)
			}
			if !ports.IsUnverifiedArtworkMiss(url, err) {
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

	chain := NewChainedArtworkResolver(
		deezer.NewDeezerAdapter(providertest.NewClient(deezerSrv.URL)),
		itunes.NewITunesAdapter(providertest.NewClient(itunesSrv.URL)),
	)
	art, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Errorf("ResolveTagged with Deezer down and no iTunes match = (%q, %v), want (\"\", ErrArtworkDegraded)", art, err)
	}
}
