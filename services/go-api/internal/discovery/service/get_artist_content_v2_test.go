package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func withDate(d string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ReleaseDate = d }
}

func withTracks(n int) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.TrackCount = n }
}

func withCover(u string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ImageURL = u }
}

func withType(t domain.RecordType) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.RecordType = t }
}

func trackFrom(provider domain.ProviderName, id, title, artist string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: artist,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
	}
}

func v2Album(provider domain.ProviderName, id, title string, opts ...func(*domain.SearchResult)) domain.SearchResult {
	r := domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: "Che",
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
		Extras:   map[string]any{},
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func TestGetAlbums_v2_bestOfMergeAndNamesakeDropped(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded",
					withDate("2026-04-01"), withCover("cover-dz"), withType("ep")),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderITunes, "i-fl", "Fully Loaded", withTracks(5)),
			}, nil
		},
	}
	namesake := consensusProvider("lastfm", "Wrong Che Single")
	consensus := NewConsensusService([]ConsensusProvider{namesake})

	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithConsensusService(consensus),
		WithContentIdentityStore(store),
	)

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1 (Fully Loaded kept, namesake dropped): %+v", len(resp.Items), resp.Items)
	}
	a := resp.Items[0]
	if a.Title != "Fully Loaded" {
		t.Fatalf("title = %q, want Fully Loaded", a.Title)
	}
	if a.Year != 2026 {
		t.Errorf("Year = %d, want 2026 (normalized from Deezer's date)", a.Year)
	}
	if a.TrackCount != 5 {
		t.Errorf("TrackCount = %d, want 5 (best-of from iTunes)", a.TrackCount)
	}
	if a.ImageURL != "cover-dz" {
		t.Errorf("ImageURL = %q, want cover-dz (best-of from Deezer)", a.ImageURL)
	}
	if rt := a.RecordType; rt != "ep" {
		t.Errorf("record_type = %q, want ep", rt)
	}
	if len(a.Sources) != 2 {
		t.Errorf("sources = %d, want 2 (deezer + itunes unioned)", len(a.Sources))
	}
}

func TestGetAlbums_v2_runsOnSeedWhenStoreHasNoBridge(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			if id != "399574001" {
				t.Errorf("deezer queried id %q, want the seed 399574001", id)
			}
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-rib", "REST IN BASS: ENCORE", withDate("2025-12-25"), withCover("c")),
			}, nil
		},
	}
	namesake := consensusProvider("lastfm", "Wrong Che Album")
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer},
		WithConsensusService(NewConsensusService([]ConsensusProvider{namesake})),
		WithContentIdentityStore(&fakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "399574001", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "REST IN BASS: ENCORE" {
		t.Fatalf("items = %+v, want just the id-verified ENCORE (namesake dropped, V2 ran on seed)", resp.Items)
	}
}

func TestGetAlbums_v2_deterministicAcrossRuns(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01")),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderITunes, "i-fl", "FULLY LOADED", withTracks(5)),
			}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
	)

	for i := 0; i < 25; i++ {
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
		if err != nil {
			t.Fatalf("run %d: GetAlbums error = %v", i, err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("run %d: items = %d, want 1 merged", i, len(resp.Items))
		}
		if got := resp.Items[0].Title; got != "Fully Loaded" {
			t.Fatalf("run %d: title = %q, want %q (Deezer is the canonical incumbent every run)", i, got, "Fully Loaded")
		}
	}
}

func TestFanOutByIdentity_slowProviderCutOffAtTimeout(t *testing.T) {
	old := detailFanOutTimeout
	detailFanOutTimeout = 50 * time.Millisecond
	defer func() { detailFanOutTimeout = old }()

	fast := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	slow := &fakeArtistContentProvider{
		getAlbumsFn: func(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: fast,
			domain.ProviderITunes: slow,
		},
		WithContentIdentityStore(store),
	)

	start := time.Now()
	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fan-out took %v, want the slow provider cut off near the 50ms timeout", elapsed)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Fully Loaded" {
		t.Fatalf("items = %+v, want just the fast provider's album (slow one degraded)", resp.Items)
	}
}

func failOnceThenEmptyProvider() *fakeArtistContentProvider {
	var albumCalls, trackCalls int
	return &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			albumCalls++
			if albumCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			trackCalls++
			if trackCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
	}
}

func fallbackPartialService(p *fakeArtistContentProvider) *GetArtistContentService {
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1"}}
	return NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p},
		WithContentIdentityStore(store),
	)
}

func TestGetAlbums_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(failOnceThenEmptyProvider()).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetTopTracks_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(failOnceThenEmptyProvider()).GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", "Che", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetArtistContent_v2_fallbackNotPartialWhenNoProviderFailed(t *testing.T) {
	svc := fallbackPartialService(&fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return nil, nil
		},
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return nil, nil
		},
	})
	albums, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if albums.Partial {
		t.Errorf("GetAlbums Partial = true, want false when no provider failed")
	}
	tracks, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", "Che", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if tracks.Partial {
		t.Errorf("GetTopTracks Partial = true, want false when no provider failed")
	}
}

func TestGetTopTracks_v2_corroboratedFirst(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				trackFrom(domain.ProviderDeezer, "d-real", "Real Song", "Che"),
				trackFrom(domain.ProviderDeezer, "d-solo", "Solo", "Che"),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{trackFrom(domain.ProviderITunes, "i-real", "Real Song", "Che")}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
	)

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", "Che", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 merged", len(resp.Items))
	}
	if resp.Items[0].Title != "Real Song" {
		t.Errorf("first = %q, want Real Song (2-provider corroboration first)", resp.Items[0].Title)
	}
}

type stubMBAnchor struct {
	titles []string
	err    error
}

func (a stubMBAnchor) ReleaseGroupTitles(context.Context, string) ([]string, error) {
	return a.titles, a.err
}

func anchoredAlbumsService(anchor stubMBAnchor, extra ...ArtistContentOption) *GetArtistContentService {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]ArtistContentOption{
		WithContentIdentityStore(&fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1"}}),
		WithMBAnchor(anchor),
	}, extra...)
	return NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, opts...)
}

func anchorFailedRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == "artist_albums.mb_anchor_failed" {
			out = append(out, rec)
		}
	}
	return out
}

func TestGetAlbums_v2_anchorErrorShipsUnfilteredAlbumsAsPartialWithOneWarn(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	leaky := &url.Error{Op: "Get", URL: "https://musicbrainz.org/ws/2/?api_key=" + secret, Err: errors.New("connection refused")}
	openBreaker := NewCircuitBreaker()
	for range failureThreshold {
		openBreaker.RecordFailure(domain.ProviderMusicBrainz)
	}
	cases := map[string]*GetArtistContentService{
		"anchor error": anchoredAlbumsService(stubMBAnchor{err: leaky}),
		"breaker open": anchoredAlbumsService(stubMBAnchor{titles: []string{"Fully Loaded"}}, WithContentCircuitBreaker(openBreaker)),
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := captureProductionLogs(t)
			resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if len(resp.Items) != 1 || !resp.Partial {
				t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
			}
			recs := anchorFailedRecords(t, buf)
			if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
				t.Fatalf("records = %v, want one WARN carrying mbid-che", recs)
			}
			if strings.Contains(buf.String(), secret) {
				t.Errorf("log leaked the secret: %s", buf.String())
			}
		})
	}
}

func TestGetAlbums_v2_anchorWithNoTitlesIsSilentNoOp(t *testing.T) {
	buf := captureProductionLogs(t)
	resp, err := anchoredAlbumsService(stubMBAnchor{}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Partial {
		t.Errorf("items = %d, partial = %v, want 1 item and partial = false", len(resp.Items), resp.Partial)
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none", recs)
	}
}

func TestGetAlbums_v2_anchorErrorAfterCallerLeftIsSilent(t *testing.T) {
	buf := captureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	svc := anchoredAlbumsService(stubMBAnchor{err: context.Canceled})
	cancel()
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if resp.Partial && len(resp.Items) > 0 {
		t.Errorf("Partial = true with items, want the anchor failure not to set it")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none", recs)
	}
}

type funcMBAnchor func(ctx context.Context, mbid string) ([]string, error)

func (f funcMBAnchor) ReleaseGroupTitles(ctx context.Context, mbid string) ([]string, error) {
	return f(ctx, mbid)
}

func anchoredAlbumsServiceWith(anchor ports.MBDiscographyAnchor, extra ...ArtistContentOption) *GetArtistContentService {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]ArtistContentOption{
		WithContentIdentityStore(&fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1"}}),
		WithMBAnchor(anchor),
	}, extra...)
	return NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, opts...)
}

func TestGetAlbums_v2_callerLeavingDuringAnchorLookupIsSilentAndNotPartial(t *testing.T) {
	buf := captureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		cancel()
		return nil, context.Canceled
	}))
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", "Che", 50)
	if !anchorCalled {
		t.Fatalf("anchor was never consulted, the test did not reach the anchor path")
	}
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with %d items, want false when the caller left before the anchor failed", len(resp.Items))
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller left", recs)
	}
}

func TestGetAlbums_v2_callerDeadlinePassingDuringAnchorLookupIsSilent(t *testing.T) {
	buf := captureProductionLogs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", "Che", 50)
	if !anchorCalled {
		t.Fatalf("anchor was never consulted, the test did not reach the anchor path")
	}
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with items, want false when the caller's own deadline passed")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller's deadline passed", recs)
	}
}

func TestGetAlbums_v2_anchorTimeoutWhileCallerStillWaitingIsPartialWithOneWarn(t *testing.T) {
	errs := map[string]error{
		"deadline exceeded":         context.DeadlineExceeded,
		"wrapped deadline exceeded": errors.Join(errors.New("mb flight"), context.DeadlineExceeded),
		"anchor's own cancel":       context.Canceled,
	}
	for name, anchorErr := range errs {
		t.Run(name, func(t *testing.T) {
			buf := captureProductionLogs(t)
			svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
				return nil, anchorErr
			}))
			resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if len(resp.Items) != 1 || !resp.Partial {
				t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
			}
			recs := anchorFailedRecords(t, buf)
			if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
				t.Fatalf("records = %v, want one WARN carrying mbid-che", recs)
			}
		})
	}
}

func TestGetAlbums_v2_openBreakerAndFailingAnchorWarnOnce(t *testing.T) {
	buf := captureProductionLogs(t)
	openBreaker := NewCircuitBreaker()
	for range failureThreshold {
		openBreaker.RecordFailure(domain.ProviderMusicBrainz)
	}
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		return nil, errors.New("mb unavailable")
	}), WithContentCircuitBreaker(openBreaker))
	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || !resp.Partial {
		t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
	}
	recs := anchorFailedRecords(t, buf)
	if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
		t.Fatalf("records = %v, want exactly one WARN carrying mbid-che", recs)
	}
}

func TestGetAlbums_v2_openBreakerAfterCallerLeftIsSilent(t *testing.T) {
	buf := captureProductionLogs(t)
	openBreaker := NewCircuitBreaker()
	for range failureThreshold {
		openBreaker.RecordFailure(domain.ProviderMusicBrainz)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			cancel()
			return []domain.SearchResult{v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer},
		WithContentIdentityStore(&fakeIdentityStore{mbid: "mbid-che", xref: map[domain.ProviderKey]string{"deezer": "d1"}}),
		WithMBAnchor(stubMBAnchor{titles: []string{"Fully Loaded"}}),
		WithContentCircuitBreaker(openBreaker),
	)
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with items, want false when the caller left")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller left", recs)
	}
}

func TestGetAlbums_v2_anchorFailureWarnCarriesNoSecretFromTheErrorURL(t *testing.T) {
	const secret = "s3cr3tVALUEfedcba9876543210"
	urls := map[string]string{
		"token query":         "https://musicbrainz.org/ws/2/release-group?artist=x&token=" + secret,
		"access_token query":  "https://musicbrainz.org/ws/2/release-group?access_token=" + secret + "&fmt=json",
		"client_secret query": "https://musicbrainz.org/ws/2/?client_secret=" + secret,
		"password query":      "https://musicbrainz.org/ws/2/?password=" + secret,
	}
	for name, u := range urls {
		t.Run(name, func(t *testing.T) {
			buf := captureProductionLogs(t)
			leaky := &url.Error{Op: "Get", URL: u, Err: errors.New("i/o timeout")}
			resp, err := anchoredAlbumsService(stubMBAnchor{err: leaky}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if !resp.Partial {
				t.Fatalf("Partial = false, want true so the warn path ran")
			}
			if recs := anchorFailedRecords(t, buf); len(recs) != 1 {
				t.Fatalf("records = %v, want one warn", recs)
			}
			if strings.Contains(buf.String(), secret) {
				t.Errorf("log leaked the secret: %s", buf.String())
			}
		})
	}
}

func TestGetAlbums_v2_anchorFailureWarnCarriesNoBearerToken(t *testing.T) {
	const token = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJl"
	buf := captureProductionLogs(t)
	anchorErr := errors.New("mb anchor: 401 for request with Authorization: Bearer " + token)
	resp, err := anchoredAlbumsService(stubMBAnchor{err: anchorErr}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true so the warn path ran")
	}
	if strings.Contains(buf.String(), token) {
		t.Errorf("log leaked the bearer token: %s", buf.String())
	}
}
