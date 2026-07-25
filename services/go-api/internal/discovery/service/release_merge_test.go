package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
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
func withType(t string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.Extras["record_type"] = t }
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
	if rt, _ := got.Result.Extras["record_type"].(string); rt != "ep" {
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

func TestBestReleaseDate_prefersPrecision(t *testing.T) {
	if got := bestReleaseDate("2020", "2020-05-01"); got != "2020-05-01" {
		t.Errorf("bestReleaseDate(year, full) = %q, want the full date", got)
	}
	if got := bestReleaseDate("2020-05-01", "2020"); got != "2020-05-01" {
		t.Errorf("bestReleaseDate(full, year) = %q, want the full date", got)
	}
}
