package release

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
	return func(r *domain.SearchResult) { r.UPC = u }
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

func TestMergeReleases_editionStaysItsOwnRelease(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(
			albumVariant(domain.ProviderDeezer, "d-base", "2 Alivë"),
			albumVariant(domain.ProviderDeezer, "d-pack", "2 Alivë (Geëk Pack)"),
		),
		idGroup(
			albumVariant(domain.ProviderITunes, "i-base", "2 Alivë", withTracks(20)),
			albumVariant(domain.ProviderITunes, "i-pack", "2 Alivë (Geëk Pack)", withTracks(29)),
		),
	}

	merged := MergeReleases(groups)

	if len(merged) != 2 {
		t.Fatalf("merged %d releases, want 2 (base and edition apart)", len(merged))
	}
	if got := findRelease(t, merged, "2 Alivë").Result.TrackCount; got != 20 {
		t.Errorf("base TrackCount = %d, want 20 (the edition's 29 leaked in)", got)
	}
	if got := findRelease(t, merged, "2 Alivë (Geëk Pack)").Result.TrackCount; got != 29 {
		t.Errorf("edition TrackCount = %d, want 29", got)
	}
}

func TestMergeReleases_versionQualifierSplitsButCreditDoesNot(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(
			albumVariant(domain.ProviderDeezer, "d1", "MELTDOWN (feat. Drake)"),
			albumVariant(domain.ProviderDeezer, "d2", "MELTDOWN (feat. Drake) (Instrumental)"),
			albumVariant(domain.ProviderDeezer, "d3", "Iris Green (feat. Future)"),
		),
		idGroup(
			albumVariant(domain.ProviderITunes, "i1", "MELTDOWN (feat. Drake)"),
			albumVariant(domain.ProviderITunes, "i2", "MELTDOWN [Instrumental] (feat. Drake)"),
			albumVariant(domain.ProviderITunes, "i3", "Iris Green (with Future)"),
		),
	}

	merged := MergeReleases(groups)

	if len(merged) != 3 {
		t.Fatalf("merged %d releases, want 3 (song, its instrumental, Iris Green)", len(merged))
	}
	for _, title := range []string{"MELTDOWN (feat. Drake)", "MELTDOWN (feat. Drake) (Instrumental)", "Iris Green (feat. Future)"} {
		if got := len(findRelease(t, merged, title).Providers); got != 2 {
			t.Errorf("%q providers = %d, want 2 (same release on both)", title, got)
		}
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
	withArtworkSource := func(s domain.ProviderKey) func(*domain.SearchResult) {
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

func withMBID(id string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ReleaseGroupMBID = id }
}

func withYear(y int) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.Year = y }
}

func TestMergeReleases_differentMBIDsStayApart(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(
			albumVariant(domain.ProviderMusicBrainz, "mb-blue", "Weezer", withMBID("blue"), withDate("1994-05-10"), withCover("blue.jpg")),
			albumVariant(domain.ProviderMusicBrainz, "mb-green", "Weezer", withMBID("green"), withDate("2001-05-15"), withCover("green.jpg")),
		),
		idGroup(albumVariant(domain.ProviderSpotify, "sp-blue", "Weezer", withDate("1994"))),
	}

	merged := MergeReleases(groups)

	if len(merged) != 2 {
		t.Fatalf("merged %d releases, want 2 (Blue and Green apart)", len(merged))
	}
	wantCover := map[string]string{"blue": "blue.jpg", "green": "green.jpg"}
	for _, m := range merged {
		if m.Result.ImageURL != wantCover[m.Result.ReleaseGroupMBID] {
			t.Errorf("MBID %q cover = %q, want %q (artwork leaked across albums)", m.Result.ReleaseGroupMBID, m.Result.ImageURL, wantCover[m.Result.ReleaseGroupMBID])
		}
	}
}

func TestMergeReleases_differentUPCsStayApart(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderITunes, "i1", "Weezer", withUPC("111"))),
		idGroup(albumVariant(domain.ProviderDeezer, "d1", "Weezer", withUPC("222"))),
	}

	if n := len(MergeReleases(groups)); n != 2 {
		t.Errorf("merged %d releases, want 2 (different UPCs)", n)
	}
}

func TestMergeReleases_yearFollowsTheMergedDate(t *testing.T) {
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderSpotify, "sp", "Pinkerton", withYear(2011))),
		idGroup(albumVariant(domain.ProviderMusicBrainz, "mb", "Pinkerton", withDate("1996-09-24"))),
	}

	got := findRelease(t, MergeReleases(groups), "Pinkerton")

	if got.Result.ReleaseDate != "1996-09-24" || got.Result.Year != 1996 {
		t.Errorf("date %q year %d, want 1996-09-24 and 1996", got.Result.ReleaseDate, got.Result.Year)
	}
}

func TestBestArtwork_prefersIdentityCoverOverPlainProviderCover(t *testing.T) {
	a := albumVariant(domain.ProviderDeezer, "d1", "No Idea", withCover("https://deezer/plain.jpg"))
	a.ArtworkSource = "deezer"
	b := albumVariant(domain.ProviderMusicBrainz, "m1", "No Idea", withCover("https://caa/identity.jpg"))
	b.ReleaseGroupMBID = "mbid-noidea"
	b.ArtworkSource = "coverartarchive"

	url, source := bestArtwork(a, b)
	if url != "https://caa/identity.jpg" || source != "coverartarchive" {
		t.Errorf("bestArtwork = (%q, %q), want the identity-pinned cover to win", url, source)
	}

	if url, source := bestArtwork(b, a); url != "https://caa/identity.jpg" || source != "coverartarchive" {
		t.Errorf("bestArtwork(b, a) = (%q, %q), want the identity-pinned cover to win", url, source)
	}
}

func TestBestArtwork_fallsBackToTheOnlyCoverForCoverage(t *testing.T) {
	a := albumVariant(domain.ProviderMusicBrainz, "m1", "No Idea")
	a.ReleaseGroupMBID = "mbid-noidea"
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

func TestSourceKey_distinguishesIDPrefixes(t *testing.T) {
	if SourceKey(domain.ProviderDeezer, "1") == SourceKey(domain.ProviderDeezer, "11") {
		t.Error(`SourceKey("deezer","1") collides with SourceKey("deezer","11")`)
	}
}

func release(title string, recordType domain.RecordType, trackCount int) MergedRelease {
	return MergedRelease{Result: domain.SearchResult{Title: title, TrackCount: trackCount, RecordType: recordType, Extras: map[string]any{}}}
}

func TestNormalizeRecordType(t *testing.T) {
	tests := []struct {
		name       string
		recordType domain.RecordType
		trackCount int
		want       domain.RecordType
	}{
		{"explicit single", "single", 0, "single"},
		{"explicit ep", "ep", 5, "ep"},
		{"explicit album", "album", 12, "album"},
		{"compilation folds into album", "compilation", 20, "album"},
		{"unknown defaults to album", "", 0, "album"},
		{"one-track album is a single", "album", 1, "single"},
		{"one-track unknown is a single", "", 1, "single"},
	}
	for _, tt := range tests {
		got := NormalizeRecordType(release(tt.name, tt.recordType, tt.trackCount))
		if got != tt.want {
			t.Errorf("%s: NormalizeRecordType = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func keepCase(providers int, idVerified, strongID bool) MergedRelease {
	ps := map[domain.ProviderName]bool{}
	names := []domain.ProviderName{domain.ProviderDeezer, domain.ProviderLastFM, domain.ProviderITunes}
	for i := 0; i < providers && i < len(names); i++ {
		ps[names[i]] = true
	}
	return MergedRelease{Providers: ps, IDVerified: idVerified, HasStrongID: strongID}
}

func TestKeepRelease(t *testing.T) {
	tests := []struct {
		name string
		m    MergedRelease
		want bool
	}{
		{"id-verified (ENCORE)", keepCase(1, true, false), true},
		{"name-fetched single source, no id (namesake)", keepCase(1, false, false), false},
		{"two by-name providers, no id-verified (still a namesake)", keepCase(2, false, false), false},
		{"strong id but not id-verified (namesake with own MBID)", keepCase(1, false, true), false},
		{"id-verified + by-name enrichment", keepCase(2, true, true), true},
	}
	for _, tt := range tests {
		if got := KeepRelease(tt.m); got != tt.want {
			t.Errorf("%s: KeepRelease = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFilterKept_dropsOnlyTheNamesake(t *testing.T) {
	releases := []MergedRelease{
		{Result: domain.SearchResult{Title: "Empty Clip"}, IDVerified: true, Providers: map[domain.ProviderName]bool{domain.ProviderDeezer: true}},
		{Result: domain.SearchResult{Title: "Wrong Che Single"}, Providers: map[domain.ProviderName]bool{domain.ProviderLastFM: true}},
		{Result: domain.SearchResult{Title: "REST IN BASS: ENCORE"}, IDVerified: true, Providers: map[domain.ProviderName]bool{domain.ProviderDeezer: true}},
	}
	kept := FilterKept(releases)
	if len(kept) != 2 {
		t.Fatalf("kept %d, want 2 (namesake dropped, both real releases kept)", len(kept))
	}
	for _, m := range kept {
		if m.Result.Title == "Wrong Che Single" {
			t.Error("namesake survived the filter")
		}
	}
}

func TestMergeReleases_albumAndSameTitledSingleStayApart(t *testing.T) {
	got := MergeReleases([]ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "d-album", "Lover", withType("album"), withTracks(18))),
		idGroup(albumVariant(domain.ProviderDeezer, "d-single", "Lover", withType("single"), withTracks(1))),
	})
	if len(got) != 2 {
		t.Fatalf("want album and single as two cards, got %d: %+v", len(got), got)
	}
}

func TestMergeReleases_sameAlbumAcrossProvidersStillMerges(t *testing.T) {
	got := MergeReleases([]ReleaseGroup{
		idGroup(albumVariant(domain.ProviderDeezer, "d1", "Lover", withType("album"))),
		idGroup(albumVariant(domain.ProviderAppleMusic, "a1", "Lover", withType("album"))),
	})
	if len(got) != 1 {
		t.Fatalf("want one merged card, got %d", len(got))
	}
}

func TestMergeReleases_releaseAndReleaseGroupMBIDsAreNotCompared(t *testing.T) {
	withReleaseGroup := func(id string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.ReleaseGroupMBID = id }
	}
	withRelease := func(id string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.ReleaseMBID = id }
	}
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderMusicBrainz, "mb", "Pinkerton", withReleaseGroup("rg-uuid"))),
		idGroup(albumVariant(domain.ProviderLastFM, "lf", "Pinkerton", withRelease("release-uuid"))),
	}

	merged := MergeReleases(groups)

	if len(merged) != 1 {
		t.Fatalf("merged %d releases, want 1 (a release MBID must not be compared with a release-group MBID)", len(merged))
	}
	if got := merged[0].Result; got.ReleaseGroupMBID != "rg-uuid" || got.ReleaseMBID != "release-uuid" {
		t.Errorf("merged MBIDs = rg %q release %q, want both carried", got.ReleaseGroupMBID, got.ReleaseMBID)
	}
}

func TestMergeReleases_differentReleaseGroupMBIDsStillStayApart(t *testing.T) {
	withReleaseGroup := func(id string) func(*domain.SearchResult) {
		return func(r *domain.SearchResult) { r.ReleaseGroupMBID = id }
	}
	groups := []ReleaseGroup{
		idGroup(albumVariant(domain.ProviderMusicBrainz, "mb-blue", "Weezer", withReleaseGroup("blue"))),
		idGroup(albumVariant(domain.ProviderMusicBrainz, "mb-green", "Weezer", withReleaseGroup("green"))),
	}

	if n := len(MergeReleases(groups)); n != 2 {
		t.Errorf("merged %d releases, want 2 (different release groups)", n)
	}
}
