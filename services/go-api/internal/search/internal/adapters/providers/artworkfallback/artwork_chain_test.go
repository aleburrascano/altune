package artworkfallback_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type slowArtworkLookup struct {
	perCallDelay time.Duration
}

func (s *slowArtworkLookup) Resolve(ctx context.Context, _ search.ResultKind, _, _, _ string) (string, error) {
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

	resolvers := make([]search.ArtworkLookup, resolverCount)
	for i := range resolvers {
		resolvers[i] = &slowArtworkLookup{perCallDelay: perCall}
	}
	const aggregate = 500 * time.Millisecond
	chain := search.NewChainedArtworkResolverWithin(aggregate, resolvers...)

	start := time.Now()
	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if !errors.Is(err, search.ErrArtworkDegraded) {
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
	resolvers := []search.ArtworkLookup{
		&slowArtworkLookup{perCallDelay: time.Second},
		&slowArtworkLookup{perCallDelay: time.Second},
	}
	chain := search.NewChainedArtworkResolverWithin(10*time.Second, resolvers...)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _ = chain.ResolveTagged(ctx, search.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("shorter caller deadline did not win: elapsed %v", elapsed)
	}
}

func TestChainedArtworkResolver_ReturnsFirstNonEmpty(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&search.FakeArtworkLookup{URL: "", Err: nil},
		&search.FakeArtworkLookup{URL: "https://images.example.com/cover.jpg", Err: nil},
		&search.FakeArtworkLookup{URL: "https://images.example.com/other.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/cover.jpg" {
		t.Errorf("expected second resolver's URL, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsErrors(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&search.FakeArtworkLookup{URL: "", Err: fmt.Errorf("network error")},
		&search.FakeArtworkLookup{URL: "https://images.example.com/fallback.jpg", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/fallback.jpg" {
		t.Errorf("expected fallback URL after error, got %q", url)
	}
}

func TestChainedArtworkResolver_AllEmpty(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&search.FakeArtworkLookup{URL: "", Err: nil},
		&search.FakeArtworkLookup{URL: "", Err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when all resolvers return empty, got %q", url)
	}
}

func TestChainedArtworkResolver_MissAfterAFailureIsDegraded(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&search.FakeArtworkLookup{Err: fmt.Errorf("coverartarchive 503")},
		&search.FakeArtworkLookup{URL: ""},
	)

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindTrack, "Song", "Artist", "mbid")

	if !errors.Is(err, search.ErrArtworkDegraded) {
		t.Fatalf("a miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_IdentityMissAfterAFailureIsDegraded(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&fakeIdentityResolver{err: fmt.Errorf("discogs 503")},
		&fakeIdentityResolver{url: ""},
	)

	url, _, err := chain.ResolveWithIdentityTagged(
		context.Background(), search.ResultKindAlbum, "DAMN.", "Kendrick Lamar", search.ArtworkIdentity{MBID: "mbid"})

	if !errors.Is(err, search.ErrArtworkDegraded) {
		t.Fatalf("an identity miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_NoResolvers(t *testing.T) {
	chain := search.NewChainedArtworkLookup()

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL with no resolvers, got %q", url)
	}
}

func TestChainedArtworkResolver_ResolveTagged_MovesOnFromAResolverThatFindsNothing(t *testing.T) {
	chain := search.NewChainedArtworkLookup(
		&search.FakeArtworkLookup{URL: ""},
		&search.FakeArtworkLookup{URL: "https://images.example.com/real.jpg"},
	)

	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindArtist, "Artist", "", "mbid")
	if err != nil || url != "https://images.example.com/real.jpg" {
		t.Errorf("(%q, %v), want the second resolver's image", url, err)
	}
}

type fakeIdentityResolver struct {
	url        string
	err        error
	source     string
	nameCalled bool
	idCalled   bool
}

func (f *fakeIdentityResolver) Resolve(context.Context, search.ResultKind, string, string, string) (string, error) {
	f.nameCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ResolveByIdentity(context.Context, search.ResultKind, search.ArtworkIdentity) (string, error) {
	f.idCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ArtworkSource() search.ProviderKey {
	return search.ProviderKey(f.source)
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged(t *testing.T) {
	nameOnly := &search.FakeArtworkLookup{URL: "https://img/name-guess.jpg"}
	failing := &fakeIdentityResolver{err: errors.New("boom"), source: "broken"}
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}

	chain := search.NewChainedArtworkLookup(nameOnly, failing, identity)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), search.ResultKindArtist, "Che", "",
		search.ArtworkIdentity{ExternalIDs: map[search.ProviderKey]string{"discogs": "38"}},
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

func TestChainedArtworkResolver_ResolveWithIdentityTagged_MovesOnFromAResolverThatFindsNothing(t *testing.T) {
	empty := &fakeIdentityResolver{source: "spotify"}
	real := &fakeIdentityResolver{url: "https://img/real.jpg", source: "discogs"}

	chain := search.NewChainedArtworkLookup(empty, real)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), search.ResultKindArtist, "Che", "", search.ArtworkIdentity{})
	if err != nil || url != "https://img/real.jpg" || source != "discogs" {
		t.Errorf("(%q, %q, %v), want the second resolver's image", url, source, err)
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_noIdentitySourceIsEmpty(t *testing.T) {
	chain := search.NewChainedArtworkLookup(&search.FakeArtworkLookup{URL: "https://img/name.jpg"})
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), search.ResultKindArtist, "Che", "", search.ArtworkIdentity{})
	if err != nil || url != "" || source != "" {
		t.Errorf("(%q, %q, %v), want empty — no identity-capable resolver in the chain", url, source, err)
	}
}

func TestChainedArtworkResolver_ResolveTagged_skipsIdentityResolvers(t *testing.T) {
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}
	name := &search.FakeArtworkLookup{URL: "https://img/name.jpg"}

	chain := search.NewChainedArtworkLookup(identity, name)
	url, source, err := chain.ResolveTagged(context.Background(), search.ResultKindArtist, "Che", "", "")
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

	coverArtArchiveOn500 := search.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL))

	chain := search.NewChainedArtworkLookup(coverArtArchiveOn500)
	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" {
		t.Errorf("url = %q, want empty on a failing resolver", url)
	}
	if !errors.Is(err, search.ErrArtworkDegraded) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkDegraded)", err)
	}
	if !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("err = %v, want errors.Is(err, ports.ErrArtworkUnavailable)", err)
	}
}

func TestChainedArtworkResolver_CoverArtArchive404IsAVerifiedMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	chain := search.NewChainedArtworkLookup(search.NewCoverArtArchiveResolver(providertest.NewClient(srv.URL)))
	url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindAlbum, "Album", "Artist", "mbid-1")

	if url != "" || err != nil {
		t.Errorf("ResolveTagged on a CAA 404 = (%q, %v), want (\"\", nil)", url, err)
	}
	if search.IsUnverifiedArtworkMiss(url, err) {
		t.Error("IsUnverifiedArtworkMiss = true, want false so the verified miss is negative-cached")
	}
}

func TestChainedArtworkResolver_CoverArtArchiveAnswersIdentityWalkOnMBIDAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := search.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := search.NewChainedArtworkLookup(caa, search.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveWithIdentityTagged(context.Background(), search.ResultKindAlbum,
		"An unrelated title", "An unrelated subtitle", search.ArtworkIdentity{MBID: "rg-1"})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive's mbid-native answer reached from the identity walk", url)
	}
	if source != search.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, search.ProviderKeyCoverArtArchive)
	}
}

func TestChainedArtworkResolver_CoverArtArchiveStillAnswersResolveTaggedAlongsideItsIdentityWrapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caa := search.NewCoverArtArchiveResolver(providertest.NewNoFollowClient(srv.URL))
	chain := search.NewChainedArtworkLookup(caa, search.NewCoverArtArchiveIdentityResolver(caa))
	url, source, err := chain.ResolveTagged(context.Background(), search.ResultKindAlbum, "Album", "Artist", "rg-1")
	if err != nil {
		t.Fatalf("ResolveTagged: %v", err)
	}
	if url != "https://coverartarchive.org/release-group/rg-1/front-1200" {
		t.Errorf("url = %q, want CoverArtArchive still reachable from the name walk", url)
	}
	if source != search.ProviderKeyCoverArtArchive {
		t.Errorf("source = %q, want %q", source, search.ProviderKeyCoverArtArchive)
	}
}

func failingArtworkProviders(srv *httptest.Server) map[string]search.ArtworkLookup {
	return map[string]search.ArtworkLookup{
		"genius":     search.NewGeniusArtworkLookup(providertest.NewClient(srv.URL), "token"),
		"ytmusic":    search.MustNewYouTubeMusicArtworkLookup(&providertest.RedirectTransport{TargetURL: srv.URL}, clock.Sleep, random.Real),
		"theaudiodb": search.NewTheAudioDBAdapter(providertest.NewClient(srv.URL), search.WithTheAudioDBAPIKey("k")),
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
			chain := search.NewChainedArtworkLookup(broken, search.NewTheAudioDBAdapter(providertest.NewClient(healthy.URL), search.WithTheAudioDBAPIKey("k")))
			url, source, err := chain.ResolveTagged(context.Background(), search.ResultKindArtist, "sombr", "", "")
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
			chain := search.NewChainedArtworkLookup(broken, &search.FakeArtworkLookup{})
			url, _, err := chain.ResolveTagged(context.Background(), search.ResultKindArtist, "sombr", "", "")
			if url != "" || !errors.Is(err, search.ErrArtworkDegraded) || !errors.Is(err, search.ErrArtworkUnavailable) {
				t.Errorf("ResolveTagged after %s fails then a miss = (%q, %v), want ErrArtworkDegraded and ErrArtworkUnavailable", name, url, err)
			}
			if !search.IsUnverifiedArtworkMiss(url, err) {
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

	chain := search.NewChainedArtworkLookup(
		search.NewDeezerAdapter(providertest.NewClient(deezerSrv.URL)),
		search.NewITunesAdapter(providertest.NewClient(itunesSrv.URL)),
	)
	art, _, err := chain.ResolveTagged(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkDegraded) {
		t.Errorf("ResolveTagged with Deezer down and no iTunes match = (%q, %v), want (\"\", ErrArtworkDegraded)", art, err)
	}
}
