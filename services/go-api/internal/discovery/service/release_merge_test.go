package service

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func albumVariant(provider domain.ProviderName, id, title string, opts ...func(*domain.SearchResult)) domain.SearchResult {
	r := domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: "che",
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
		Extras:   map[string]any{},
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

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

func withUPC(u string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.Extras["upc"] = u }
}

func idGroup(rs ...domain.SearchResult) ReleaseGroup {
	return ReleaseGroup{Releases: rs, IDVerified: true}
}

func nameGroup(rs ...domain.SearchResult) ReleaseGroup {
	return ReleaseGroup{Releases: rs, IDVerified: false}
}

func findRelease(t *testing.T, releases []MergedRelease, title string) MergedRelease {
	t.Helper()
	for _, m := range releases {
		if m.Result.Title == title {
			return m
		}
	}
	t.Fatalf("release %q not in merged output %+v", title, releases)
	return MergedRelease{}
}

func TestMergeReleases_bestOfAcrossProviders(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "d1", "Fully Loaded", withDate("2026-04-01"), withCover("cover-dz"), withType("ep"))),
		idGroup(albumVariant(domain.ProviderAppleMusic, "a1", "Fully Loaded", withTracks(5), withType("album"))),
		idGroup(albumVariant(domain.ProviderSoundCloud, "s1", "Fully Loaded", withTracks(5), withDate("2026-04-01T00:00:00Z"))),
	}

	got := findRelease(t, MergeReleases(groups), "Fully Loaded")

	if got.Result.ReleaseDate == "" {
		t.Error("ReleaseDate dropped — the F2 bug (a dateless variant masked the date)")
	}
	if got.Result.TrackCount != 5 {
		t.Errorf("TrackCount = %d, want 5 (best-of from Apple/SoundCloud)", got.Result.TrackCount)
	}
	if got.Result.ImageURL != "cover-dz" {
		t.Errorf("ImageURL = %q, want cover-dz (best-of from Deezer)", got.Result.ImageURL)
	}
	if rt := got.Result.RecordType; rt != "ep" {
		t.Errorf("record_type = %q, want ep (specific beats generic album)", rt)
	}
	if len(got.Result.Sources) != 3 {
		t.Errorf("sources = %d, want 3 unioned", len(got.Result.Sources))
	}
	if len(got.Providers) != 3 {
		t.Errorf("providers = %d, want 3 for corroboration", len(got.Providers))
	}
}

func TestMergeReleases_singleProviderReleaseSurvivesIntact(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "enc", "REST IN BASS: ENCORE", withDate("2025-12-25"), withCover("c"), withType("album"))),
	}

	got := findRelease(t, MergeReleases(groups), "REST IN BASS: ENCORE")

	if got.Result.ReleaseDate != "2025-12-25" {
		t.Errorf("ReleaseDate = %q, want 2025-12-25 (single-source release kept intact)", got.Result.ReleaseDate)
	}
	if len(got.Providers) != 1 {
		t.Errorf("providers = %d, want 1 (single-source, for the keep step to weigh)", len(got.Providers))
	}
	if got.HasStrongID {
		t.Error("HasStrongID = true, want false (no UPC/MBID/ISRC on this variant)")
	}
}

func TestMergeReleases_coverAndYearCombine(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderLastFM, "l1", "Closed Captions", withCover("cover-lfm"))),
		idGroup(albumVariant(domain.ProviderDeezer, "d9", "closed captions", withDate("2023-07-21"))),
	}

	merged := MergeReleases(groups)
	if len(merged) != 1 {
		t.Fatalf("clusters = %d, want 1 (case-insensitive title match)", len(merged))
	}
	got := merged[0]
	if got.Result.ImageURL != "cover-lfm" {
		t.Errorf("ImageURL = %q, want cover-lfm", got.Result.ImageURL)
	}
	if got.Result.ReleaseDate != "2023-07-21" {
		t.Errorf("ReleaseDate = %q, want 2023-07-21", got.Result.ReleaseDate)
	}
}

func TestMergeReleases_adoptedArtworkKeepsItsSourceTag(t *testing.T) {
	withArtworkSource := func(s string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.ArtworkSource = s }
	}
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "d1", "Nafi", withArtworkSource("deezer-stale"))),
		idGroup(albumVariant(domain.ProviderITunes, "i1", "Nafi", withCover("cover-it"), withArtworkSource("itunes"))),
	}

	got := findRelease(t, MergeReleases(groups), "Nafi")

	if got.Result.ImageURL != "cover-it" {
		t.Errorf("ImageURL = %q, want cover-it (adopted from iTunes)", got.Result.ImageURL)
	}
	if got.Result.ArtworkSource != "itunes" {
		t.Errorf("ArtworkSource = %q, want itunes (must travel with the adopted URL)", got.Result.ArtworkSource)
	}
}

func TestMergeReleases_strongIDDetected(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderAppleMusic, "a1", "Sayso Says", withUPC("00888880000"))),
	}
	if !findRelease(t, MergeReleases(groups), "Sayso Says").HasStrongID {
		t.Error("HasStrongID = false, want true (UPC present)")
	}
}

func TestMergeReleases_strongIDDetectedFromTypedUPC(t *testing.T) {
	withTypedUPC := func(u string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.UPC = u }
	}
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderAppleMusic, "a1", "Sayso Says", withTypedUPC("00888880000"))),
	}
	got := findRelease(t, MergeReleases(groups), "Sayso Says")
	if !got.HasStrongID {
		t.Error("HasStrongID = false, want true (typed UPC present)")
	}
}

func TestMergeReleases_typedUPCFolds(t *testing.T) {
	withTypedUPC := func(u string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.UPC = u }
	}
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "d1", "Nafi")),
		idGroup(albumVariant(domain.ProviderAppleMusic, "a1", "Nafi", withTypedUPC("00888880000"))),
	}
	if got := findRelease(t, MergeReleases(groups), "Nafi"); got.Result.UPC != "00888880000" {
		t.Errorf("merged UPC = %q, want the Apple variant's typed UPC", got.Result.UPC)
	}
}

func TestBestArtwork_prefersIdentityCoverOverPlainProviderCover(t *testing.T) {
	// Two variants of the same release: a plain provider cover must lose to the
	// id-pinned one instead of winning just because it was seen first.
	a := albumVariant(domain.ProviderDeezer, "d1", "No Idea", withCover("https://deezer/plain.jpg"))
	a.ArtworkSource = "deezer"
	b := albumVariant(domain.ProviderMusicBrainz, "m1", "No Idea", withCover("https://caa/identity.jpg"))
	b.MBID = "mbid-noidea"
	b.ArtworkSource = "coverartarchive"

	url, source := bestArtwork(a, b)
	if url != "https://caa/identity.jpg" || source != "coverartarchive" {
		t.Errorf("bestArtwork = (%q, %q), want the identity-pinned cover to win", url, source)
	}

	// Order-independent: the id-pinned cover still wins when it is the first arg.
	if url, source := bestArtwork(b, a); url != "https://caa/identity.jpg" || source != "coverartarchive" {
		t.Errorf("bestArtwork(b, a) = (%q, %q), want the identity-pinned cover to win", url, source)
	}
}

func TestBestArtwork_fallsBackToTheOnlyCoverForCoverage(t *testing.T) {
	// A missing cover on the id-bearing side must still adopt the sibling's image
	// — same album, so coverage must not regress.
	a := albumVariant(domain.ProviderMusicBrainz, "m1", "No Idea")
	a.MBID = "mbid-noidea"
	b := albumVariant(domain.ProviderDeezer, "d1", "No Idea", withCover("https://deezer/cover.jpg"))
	b.ArtworkSource = "deezer"

	if url, source := bestArtwork(a, b); url != "https://deezer/cover.jpg" || source != "deezer" {
		t.Errorf("bestArtwork = (%q, %q), want the only available cover for coverage", url, source)
	}
}

func TestBestReleaseDate_prefersPrecision(t *testing.T) {
	if got := bestReleaseDate("2020", "2020-05-01"); got != "2020-05-01" {
		t.Errorf("bestReleaseDate(year, full) = %q, want the full date", got)
	}
	if got := bestReleaseDate("2020-05-01", "2020"); got != "2020-05-01" {
		t.Errorf("bestReleaseDate(full, year) = %q, want the full date", got)
	}
}

// Pins the Extras-era semantics bestOfRelease kept when record_type and
// resolution_tier became typed: an unrecognised type survives when the other
// side has none, and the receiver's tier wins only when it has one.
func TestBestOfRelease_typedRecordTypeAndTierFallBack(t *testing.T) {
	a := albumVariant(domain.ProviderDeezer, "1", "Blue")
	b := albumVariant(domain.ProviderYouTube, "2", "Blue", withType("Album"))
	b.ResolutionTier = domain.StampResolutionTier(domain.EntityResolutionNone)

	got := bestOfRelease(a, b)
	if got.RecordType != "Album" {
		t.Errorf("RecordType = %q, want the only present (unrecognised) type Album", got.RecordType)
	}
	if got.ResolutionTier != b.ResolutionTier {
		t.Errorf("ResolutionTier = %+v, want b's stamp when a has none", got.ResolutionTier)
	}

	a.ResolutionTier = domain.StampResolutionTier(domain.EntityResolutionISRC)
	if got := bestOfRelease(a, b); got.ResolutionTier != a.ResolutionTier {
		t.Errorf("ResolutionTier = %+v, want a's stamp to win", got.ResolutionTier)
	}
}
