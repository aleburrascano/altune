package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test fakes ---

type fakeMBLookup struct {
	resolveArtistIdentityFn func(ctx context.Context, name string) (*ports.ArtistIdentity, error)
	validateArtistAlbumsFn  func(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error)
	lookupAlbumArtistFn     func(ctx context.Context, artistName, albumTitle string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error)
}

func (f *fakeMBLookup) ResolveArtistIdentity(ctx context.Context, name string) (*ports.ArtistIdentity, error) {
	if f.resolveArtistIdentityFn != nil {
		return f.resolveArtistIdentityFn(ctx, name)
	}
	return nil, nil
}

func (f *fakeMBLookup) ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error) {
	if f.validateArtistAlbumsFn != nil {
		return f.validateArtistAlbumsFn(ctx, artistName, albums)
	}
	return &ports.AlbumValidationResult{Confirmed: nil, Unconfirmed: albums}, nil
}

func (f *fakeMBLookup) LookupAlbumArtist(ctx context.Context, artistName, albumTitle string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
	if f.lookupAlbumArtistFn != nil {
		return f.lookupAlbumArtistFn(ctx, artistName, albumTitle, profile)
	}
	return domain.AlbumVerdictUnknown, "", nil
}

type fakeITunesLookup struct {
	lookupAlbumFn func(ctx context.Context, albumTitle, artistName string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error)
}

func (f *fakeITunesLookup) LookupAlbum(ctx context.Context, albumTitle, artistName string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
	if f.lookupAlbumFn != nil {
		return f.lookupAlbumFn(ctx, albumTitle, artistName, profile)
	}
	return domain.AlbumVerdictUnknown, 0, nil
}

type fakeISRCFetcher struct {
	fetchTrackISRCFn    func(ctx context.Context, trackID string) (string, error)
	fetchFirstTrackIDFn func(ctx context.Context, albumID string) (string, error)
}

func (f *fakeISRCFetcher) FetchTrackISRC(ctx context.Context, trackID string) (string, error) {
	if f.fetchTrackISRCFn != nil {
		return f.fetchTrackISRCFn(ctx, trackID)
	}
	return "", nil
}

func (f *fakeISRCFetcher) FetchFirstTrackID(ctx context.Context, albumID string) (string, error) {
	if f.fetchFirstTrackIDFn != nil {
		return f.fetchFirstTrackIDFn(ctx, albumID)
	}
	return "track-1", nil
}

type fakeIdentityCache struct {
	entries map[string]fakeCacheEntry
}

type fakeCacheEntry struct {
	verdict   domain.AlbumVerdict
	reason    string
	layer     string
	firstSeen time.Time
}

func newFakeIdentityCache() *fakeIdentityCache {
	return &fakeIdentityCache{entries: map[string]fakeCacheEntry{}}
}

func (f *fakeIdentityCache) GetVerdict(_ context.Context, artistName, albumTitle string) (domain.AlbumVerdict, string, string, time.Time, bool) {
	key := artistName + "|" + albumTitle
	e, ok := f.entries[key]
	if !ok {
		return domain.AlbumVerdictUnknown, "", "", time.Time{}, false
	}
	return e.verdict, e.reason, e.layer, e.firstSeen, true
}

func (f *fakeIdentityCache) SetVerdict(_ context.Context, artistName, albumTitle string, verdict domain.AlbumVerdict, reason, layer string) {
	key := artistName + "|" + albumTitle
	existing, ok := f.entries[key]
	firstSeen := time.Now()
	if ok {
		firstSeen = existing.firstSeen
	}
	f.entries[key] = fakeCacheEntry{
		verdict:   verdict,
		reason:    reason,
		layer:     layer,
		firstSeen: firstSeen,
	}
}

// --- helpers ---

func testAlbum(title string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:   domain.ResultKindAlbum,
		Title:  title,
		Extras: extras,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderDeezer,
			ExternalID: "123",
		}},
	}
}

func testProfile(mbid string, birthYear int) domain.ArtistIdentityProfile {
	p := domain.NewArtistIdentityProfile()
	p.MBID = mbid
	p.BirthYear = birthYear
	return p
}

// --- tests ---

func TestIdentityResolver_confirmed_by_mb_release_group(t *testing.T) {
	svc := NewIdentityResolverService()
	profile := testProfile("mbid-123", 2006)
	profile.MBConfirmedTitles[NormalizeForMatch("REST IN BASS")] = true

	albums := []domain.SearchResult{
		testAlbum("REST IN BASS", map[string]any{"year": 2022}),
		testAlbum("Unknown Album", map[string]any{"year": 2023}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 2)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict)
	assert.Equal(t, "mb", resolutions[0].Layer)
	assert.Equal(t, domain.AlbumVerdictUnknown, resolutions[1].Verdict)
}

func TestIdentityResolver_confirmed_by_r2_reverse_lookup(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, albumTitle string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			if albumTitle == "Sayso Says" {
				return domain.AlbumVerdictConfirmed, "mbid-123", nil
			}
			return domain.AlbumVerdictUnknown, "", nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("Sayso Says", map[string]any{"year": 2021}),
	}

	// R2 confirms — iTunes should not be called
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			t.Error("itunes should not have been called after R2 confirmed")
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}
	svc = NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict)
	assert.Equal(t, "mb", resolutions[0].Layer)
}

func TestIdentityResolver_contamination_by_r2_different_mbid(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			return domain.AlbumVerdictContamination, "other-mbid", nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("LOTTO DREAMS", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "mb", resolutions[0].Layer)
}

func TestIdentityResolver_confirmed_by_itunes_r3(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			return domain.AlbumVerdictUnknown, "", nil // R2 unknown
		},
	}
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			return domain.AlbumVerdictConfirmed, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("New Album", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict)
	assert.Equal(t, "itunes", resolutions[0].Layer)
}

func TestIdentityResolver_contamination_by_itunes_r3(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			return domain.AlbumVerdictUnknown, "", nil
		},
	}
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			return domain.AlbumVerdictContamination, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("Tšernobõl", map[string]any{"year": 2001, "genre": "Rock"}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "itunes", resolutions[0].Layer)
}

func TestIdentityResolver_contamination_by_temporal_r3b(t *testing.T) {
	// R2 and R3 both return unknown
	mb := &fakeMBLookup{}
	itunes := &fakeITunesLookup{}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))

	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("Old Album", map[string]any{"year": 1995}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "temporal", resolutions[0].Layer)
	assert.Contains(t, resolutions[0].Reason, "predates")
}

func TestIdentityResolver_contamination_by_genre_r3b(t *testing.T) {
	svc := NewIdentityResolverService()

	profile := testProfile("", 0)
	profile.AddGenre("hip-hop")
	profile.AddGenre("rap")

	albums := []domain.SearchResult{
		testAlbum("Rock Album", map[string]any{"genre": "Rock"}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "genre", resolutions[0].Layer)
}

func TestIdentityResolver_contamination_by_artist_type_r3b(t *testing.T) {
	svc := NewIdentityResolverService()

	profile := testProfile("", 0)
	profile.ArtistType = "Person"

	albums := []domain.SearchResult{
		testAlbum("Group Album", map[string]any{"artist_type": "Group"}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "type", resolutions[0].Layer)
}

func TestIdentityResolver_contamination_by_isrc_r3c(t *testing.T) {
	isrcFetcher := &fakeISRCFetcher{
		fetchTrackISRCFn: func(_ context.Context, _ string) (string, error) {
			return "QZFZ62070654", nil // registrant FZ62, not in known set
		},
	}

	svc := NewIdentityResolverService(WithISRCFetcher(isrcFetcher))

	profile := testProfile("", 0)
	profile.AddISRCRegistrant("J842")

	albums := []domain.SearchResult{
		testAlbum("Gallos Ciegos", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictContamination, resolutions[0].Verdict)
	assert.Equal(t, "isrc", resolutions[0].Layer)
	assert.Equal(t, "isrc registrant mismatch", resolutions[0].Reason)
}

func TestIdentityResolver_isrc_match_with_multiple_registrants(t *testing.T) {
	isrcFetcher := &fakeISRCFetcher{
		fetchTrackISRCFn: func(_ context.Context, _ string) (string, error) {
			return "QZSE6A2070654", nil // registrant SE6A, in known set
		},
	}

	svc := NewIdentityResolverService(WithISRCFetcher(isrcFetcher))

	profile := testProfile("", 0)
	profile.AddISRCRegistrant("J842")
	profile.AddISRCRegistrant("SE6A")

	albums := []domain.SearchResult{
		testAlbum("DRUNKEN LOVE", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictUnknown, resolutions[0].Verdict, "should pass ISRC check with known registrant")
}

func TestIdentityResolver_all_checks_pass_returns_unknown(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			return domain.AlbumVerdictUnknown, "", nil
		},
	}
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("Brand New Release", map[string]any{"year": 2025}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictUnknown, resolutions[0].Verdict)
}

func TestIdentityResolver_cache_hit_returns_cached_verdict(t *testing.T) {
	cache := newFakeIdentityCache()
	cache.entries["Che|REST IN BASS"] = fakeCacheEntry{
		verdict:   domain.AlbumVerdictConfirmed,
		reason:    "mb release-group match",
		layer:     "mb",
		firstSeen: time.Now(),
	}

	// No providers — cache should short-circuit
	svc := NewIdentityResolverService(WithIdentityCache(cache))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("REST IN BASS", map[string]any{"year": 2022}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict)
	assert.Equal(t, "mb", resolutions[0].Layer)
}

func TestIdentityResolver_mb_error_falls_through_to_r3(t *testing.T) {
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			return domain.AlbumVerdictUnknown, "", nil // graceful degradation
		},
	}
	itunesCalled := false
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			itunesCalled = true
			return domain.AlbumVerdictConfirmed, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)

	albums := []domain.SearchResult{
		testAlbum("Some Album", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	assert.True(t, itunesCalled, "itunes should be called when MB returns unknown")
	require.Len(t, resolutions, 1)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict)
}

func TestIdentityResolver_full_pipeline_mixed_albums(t *testing.T) {
	mb := &fakeMBLookup{
		validateArtistAlbumsFn: func(_ context.Context, _ string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error) {
			var confirmed, unconfirmed []domain.SearchResult
			for _, a := range albums {
				if a.Title == "REST IN BASS" || a.Title == "Sayso Says" {
					confirmed = append(confirmed, a)
				} else {
					unconfirmed = append(unconfirmed, a)
				}
			}
			return &ports.AlbumValidationResult{
				Confirmed:   confirmed,
				Unconfirmed: unconfirmed,
			}, nil
		},
		lookupAlbumArtistFn: func(_ context.Context, _, albumTitle string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			if albumTitle == "LOTTO DREAMS" {
				return domain.AlbumVerdictContamination, "other-mbid", nil
			}
			return domain.AlbumVerdictUnknown, "", nil
		},
	}
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, albumTitle, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			if albumTitle == "Tšernobõl" {
				return domain.AlbumVerdictContamination, 0, nil
			}
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)
	profile.MBConfirmedTitles[NormalizeForMatch("REST IN BASS")] = true
	profile.MBConfirmedTitles[NormalizeForMatch("Sayso Says")] = true

	albums := []domain.SearchResult{
		testAlbum("REST IN BASS", map[string]any{"year": 2022}),
		testAlbum("Sayso Says", map[string]any{"year": 2021}),
		testAlbum("LOTTO DREAMS", map[string]any{"year": 2024}),
		testAlbum("Tšernobõl", map[string]any{"year": 2001, "genre": "Rock"}),
		testAlbum("Samsonite", map[string]any{"year": 1995}), // temporal impossibility
		testAlbum("Brand New", map[string]any{"year": 2025}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 6)

	verdicts := map[string]domain.AlbumVerdict{}
	for _, r := range resolutions {
		verdicts[r.Album.Title] = r.Verdict
	}

	assert.Equal(t, domain.AlbumVerdictConfirmed, verdicts["REST IN BASS"])
	assert.Equal(t, domain.AlbumVerdictConfirmed, verdicts["Sayso Says"])
	assert.Equal(t, domain.AlbumVerdictContamination, verdicts["LOTTO DREAMS"])
	assert.Equal(t, domain.AlbumVerdictContamination, verdicts["Tšernobõl"])
	assert.Equal(t, domain.AlbumVerdictContamination, verdicts["Samsonite"])
	assert.Equal(t, domain.AlbumVerdictUnknown, verdicts["Brand New"])
}

func TestIdentityResolver_all_providers_fail_returns_all_unknown(t *testing.T) {
	// No providers configured at all
	svc := NewIdentityResolverService()

	profile := testProfile("", 0)

	albums := []domain.SearchResult{
		testAlbum("Album A", nil),
		testAlbum("Album B", nil),
	}

	resolutions := svc.Resolve(context.Background(), "SomeArtist", profile, albums)

	require.Len(t, resolutions, 2)
	for _, r := range resolutions {
		assert.Equal(t, domain.AlbumVerdictUnknown, r.Verdict, "album %s should be unknown", r.Album.Title)
	}
}

func TestIdentityResolver_no_mbid_skips_r2(t *testing.T) {
	r2Called := false
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			r2Called = true
			return domain.AlbumVerdictUnknown, "", nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb))
	// Profile with no MBID — R2 should be skipped
	profile := testProfile("", 0)

	albums := []domain.SearchResult{
		testAlbum("Some Album", map[string]any{"year": 2024}),
	}

	svc.Resolve(context.Background(), "Artist", profile, albums)

	assert.False(t, r2Called, "R2 should not be called when profile has no MBID")
}

func TestBuildProfile_accumulates_signals(t *testing.T) {
	mb := &fakeMBLookup{
		resolveArtistIdentityFn: func(_ context.Context, _ string) (*ports.ArtistIdentity, error) {
			return &ports.ArtistIdentity{
				MBID:           "test-mbid",
				BirthYear:      1990,
				Disambiguation: "rapper",
			}, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb))

	albums := []domain.SearchResult{
		testAlbum("Album 1", map[string]any{"genre": "Hip-Hop/Rap"}),
		testAlbum("Album 2", map[string]any{"genre_id": 116}), // Deezer rap/hip hop
		testAlbum("Album 3", map[string]any{"genre_id": 116}),
		testAlbum("Album 4", map[string]any{"genre_id": 116}),
	}

	profile := svc.BuildProfile(context.Background(), "TestArtist", albums)

	assert.Equal(t, "test-mbid", profile.MBID)
	assert.Equal(t, 1990, profile.BirthYear)
	assert.Equal(t, "rapper", profile.Disambiguation)
	assert.True(t, profile.GenreCluster["hip hop"], "hip hop should be dominant (4/4 albums)")
	assert.True(t, profile.GenreCluster["rap"], "rap should be dominant (4/4 albums)")
}

func TestBuildProfile_collects_multiple_isrc_registrants(t *testing.T) {
	isrcByAlbum := map[string]string{
		"album-1": "QZJ842503215", // registrant J842
		"album-2": "QZSE6A270654", // registrant SE6A
		"album-3": "QZJ842600001", // registrant J842 (duplicate)
	}
	mb := &fakeMBLookup{
		resolveArtistIdentityFn: func(_ context.Context, _ string) (*ports.ArtistIdentity, error) {
			return &ports.ArtistIdentity{MBID: "test-mbid"}, nil
		},
		validateArtistAlbumsFn: func(_ context.Context, _ string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error) {
			return &ports.AlbumValidationResult{Confirmed: albums}, nil
		},
	}
	isrcFetcher := &fakeISRCFetcher{
		fetchFirstTrackIDFn: func(_ context.Context, albumID string) (string, error) {
			return "track-" + albumID, nil
		},
		fetchTrackISRCFn: func(_ context.Context, trackID string) (string, error) {
			albumID := trackID[len("track-"):]
			if isrc, ok := isrcByAlbum[albumID]; ok {
				return isrc, nil
			}
			return "", nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithISRCFetcher(isrcFetcher))

	albums := []domain.SearchResult{
		{Title: "Album 1", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "album-1"}}},
		{Title: "Album 2", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "album-2"}}},
		{Title: "Album 3", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "album-3"}}},
	}

	profile := svc.BuildProfile(context.Background(), "Che", albums)

	assert.Len(t, profile.KnownISRCRegistrants, 2, "should collect J842 and SE6A")
	assert.True(t, profile.KnownISRCRegistrants["J842"])
	assert.True(t, profile.KnownISRCRegistrants["SE6A"])
}

func TestIdentityResolver_itunes_artist_id_mismatch_returns_unknown(t *testing.T) {
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, albumTitle, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			switch albumTitle {
			case "Real Album":
				return domain.AlbumVerdictConfirmed, 111, nil // Che artist ID 111
			case "Vergif":
				return domain.AlbumVerdictConfirmed, 999, nil // different Che, artist ID 999
			}
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithITunesLookup(itunes))
	profile := testProfile("", 0)

	albums := []domain.SearchResult{
		testAlbum("Real Album", map[string]any{"year": 2024}),
		testAlbum("Vergif", map[string]any{"year": 2024}),
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 2)
	assert.Equal(t, domain.AlbumVerdictConfirmed, resolutions[0].Verdict, "first album sets the artist ID")
	assert.Equal(t, domain.AlbumVerdictUnknown, resolutions[1].Verdict, "different artist ID should not confirm")
}

func TestIdentityResolver_skips_r2_after_consecutive_mb_errors(t *testing.T) {
	mbCallCount := 0
	mb := &fakeMBLookup{
		lookupAlbumArtistFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
			mbCallCount++
			return domain.AlbumVerdictUnknown, "", fmt.Errorf("mb: timeout")
		},
	}
	itunesCallCount := 0
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, _, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			itunesCallCount++
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb), WithITunesLookup(itunes))
	profile := testProfile("mbid-123", 2006)

	albums := make([]domain.SearchResult, 5)
	for i := range albums {
		albums[i] = testAlbum(fmt.Sprintf("Album %d", i), map[string]any{"year": 2024})
	}

	resolutions := svc.Resolve(context.Background(), "Che", profile, albums)

	require.Len(t, resolutions, 5)
	assert.Equal(t, 3, mbCallCount, "MB should be called 3 times before being skipped")
	assert.Equal(t, 5, itunesCallCount, "iTunes should be called for all 5 albums")
}

func TestIdentityResolver_pass2_enriches_isrc_from_itunes_confirmed(t *testing.T) {
	// MB is down — no confirmed albums from BuildProfile, ISRC set empty
	// iTunes confirms 2 albums in pass 1
	// Pass 2 samples ISRCs from those, catches contamination album
	itunes := &fakeITunesLookup{
		lookupAlbumFn: func(_ context.Context, albumTitle, _ string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, int64, error) {
			switch albumTitle {
			case "Real Album A", "Real Album B":
				return domain.AlbumVerdictConfirmed, 0, nil
			}
			return domain.AlbumVerdictUnknown, 0, nil
		},
	}

	isrcByAlbum := map[string]string{
		"dz-real-a":    "QZJ842503215", // registrant J842
		"dz-real-b":    "QZJ842600001", // registrant J842
		"dz-contam":    "CHXX99070654", // registrant XX99 — different
		"dz-also-real": "QZJ842700002", // registrant J842 — same
	}
	isrcFetcher := &fakeISRCFetcher{
		fetchFirstTrackIDFn: func(_ context.Context, albumID string) (string, error) {
			return "track-" + albumID, nil
		},
		fetchTrackISRCFn: func(_ context.Context, trackID string) (string, error) {
			albumID := trackID[len("track-"):]
			if isrc, ok := isrcByAlbum[albumID]; ok {
				return isrc, nil
			}
			return "", nil
		},
	}

	svc := NewIdentityResolverService(WithITunesLookup(itunes), WithISRCFetcher(isrcFetcher))
	profile := testProfile("", 0) // no MBID, no confirmed albums

	albums := []domain.SearchResult{
		{Title: "Real Album A", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "dz-real-a"}}},
		{Title: "Real Album B", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "dz-real-b"}}},
		{Title: "Contamination", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "dz-contam"}}},
		{Title: "Also Real", Kind: domain.ResultKindAlbum, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "dz-also-real"}}},
	}

	resolutions := svc.Resolve(context.Background(), "TestArtist", profile, albums)

	require.Len(t, resolutions, 4)

	verdicts := map[string]domain.AlbumVerdict{}
	for _, r := range resolutions {
		verdicts[r.Album.Title] = r.Verdict
	}

	assert.Equal(t, domain.AlbumVerdictConfirmed, verdicts["Real Album A"], "confirmed by iTunes in pass 1")
	assert.Equal(t, domain.AlbumVerdictConfirmed, verdicts["Real Album B"], "confirmed by iTunes in pass 1")
	assert.Equal(t, domain.AlbumVerdictContamination, verdicts["Contamination"], "caught by ISRC in pass 2")
	assert.Equal(t, domain.AlbumVerdictUnknown, verdicts["Also Real"], "same registrant — not flagged")
}

func TestBuildProfile_handles_nil_mb(t *testing.T) {
	svc := NewIdentityResolverService() // no MB

	albums := []domain.SearchResult{
		testAlbum("Album 1", map[string]any{"genre": "Pop"}),
		testAlbum("Album 2", map[string]any{"genre": "Pop"}),
		testAlbum("Album 3", map[string]any{"genre": "Pop"}),
	}

	profile := svc.BuildProfile(context.Background(), "TestArtist", albums)

	assert.Equal(t, "", profile.MBID)
	assert.Equal(t, 0, profile.BirthYear)
	assert.True(t, profile.GenreCluster["pop"], "pop should be dominant (3/3 albums)")
}

func TestResolveDiscographyIdentity_ordering(t *testing.T) {
	mb := &fakeMBLookup{
		resolveArtistIdentityFn: func(_ context.Context, _ string) (*ports.ArtistIdentity, error) {
			return &ports.ArtistIdentity{MBID: "mbid-123", BirthYear: 2006}, nil
		},
		validateArtistAlbumsFn: func(_ context.Context, _ string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error) {
			var confirmed, unconfirmed []domain.SearchResult
			for _, a := range albums {
				if a.Title == "Confirmed Album" {
					confirmed = append(confirmed, a)
				} else {
					unconfirmed = append(unconfirmed, a)
				}
			}
			return &ports.AlbumValidationResult{
				Confirmed:   confirmed,
				Unconfirmed: unconfirmed,
			}, nil
		},
	}

	svc := NewIdentityResolverService(WithMBLookup(mb))
	resolver := &GetArtistContentService{
		identityResolver: svc,
	}

	albums := []domain.SearchResult{
		testAlbum("Unknown Album", map[string]any{"year": 2025}),
		testAlbum("Confirmed Album", map[string]any{"year": 2022}),
		testAlbum("Old Contamination", map[string]any{"year": 1990}), // temporal
	}

	results := resolver.resolveDiscographyIdentity(context.Background(), "TestArtist", albums)

	// Contamination removed, confirmed first, unknown after
	require.Len(t, results, 2)
	assert.Equal(t, "Confirmed Album", results[0].Title)
	assert.Equal(t, "Unknown Album", results[1].Title)
}

func TestExtractDeezerAlbumID(t *testing.T) {
	tests := []struct {
		name     string
		album    domain.SearchResult
		expected string
	}{
		{
			name: "from deezer source",
			album: domain.SearchResult{
				Sources: []domain.SourceRef{{
					Provider:   domain.ProviderDeezer,
					ExternalID: "album-456",
				}},
			},
			expected: "album-456",
		},
		{
			name: "no deezer source",
			album: domain.SearchResult{
				Sources: []domain.SourceRef{{
					Provider:   domain.ProviderMusicBrainz,
					ExternalID: "mb-id",
				}},
			},
			expected: "",
		},
		{
			name:     "empty album",
			album:    domain.SearchResult{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDeezerAlbumID(tt.album)
			assert.Equal(t, tt.expected, got)
		})
	}
}
