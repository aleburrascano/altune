package discoverybridge

import (
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryports "altune/go-api/internal/discovery/ports"
)

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type stubSearcher struct {
	out *search.SearchOutput
}

func (s stubSearcher) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*search.SearchOutput, error) {
	return s.out, nil
}

func TestProviderKey_IsByteIdenticalToDiscoveryString(t *testing.T) {
	for p := discoverydomain.ProviderUnknown; p <= discoverydomain.ProviderSpotify; p++ {
		if got, want := string(providerKey(p)), p.String(); got != want {
			t.Errorf("providerKey(%v) = %q, want %q", p, got, want)
		}
	}
}

func TestResolve_SourcesAreKeyedBySharedProviderConstants(t *testing.T) {
	resolver := NewRecordingResolver(stubSearcher{out: &search.SearchOutput{
		Results: []discoverydomain.SearchResult{{
			Kind:     discoverydomain.ResultKindTrack,
			Title:    "Song",
			Subtitle: "Artist",
			Sources: []discoverydomain.SourceRef{
				{Provider: discoverydomain.ProviderYouTube, ExternalID: "vid123"},
				{Provider: discoverydomain.ProviderDeezer, ExternalID: "3135556"},
				{Provider: discoverydomain.ProviderSoundCloud, ExternalID: "999"},
			},
		}},
	}})

	identity, err := resolver.Resolve(context.Background(), acqports.RecordingQuery{Title: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for key, wantID := range map[acqports.RecordingProvider]string{
		acqports.ProviderYouTube:    "vid123",
		acqports.ProviderDeezer:     "3135556",
		acqports.ProviderSoundCloud: "999",
	} {
		got, ok := identity.SourceFor(key)
		if !ok || got.ExternalID != wantID {
			t.Errorf("SourceFor(%q) = %+v, %v; want ExternalID %q", key, got, ok, wantID)
		}
	}
}

type stubISRCAuthority struct {
	recordings []discoveryports.ISRCRecording
	err        error
	calls      int
}

func (s *stubISRCAuthority) RecordingsByISRC(context.Context, string) ([]discoveryports.ISRCRecording, error) {
	s.calls++
	return s.recordings, s.err
}

type failingSearcher struct{}

func (failingSearcher) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*search.SearchOutput, error) {
	return nil, errors.New("search down")
}

func drinkingInLA() stubSearcher {
	return stubSearcher{out: &search.SearchOutput{
		Results: []discoverydomain.SearchResult{{
			Kind:          discoverydomain.ResultKindTrack,
			Title:         "Drinking in L.A.",
			Subtitle:      "Bran Van 3000",
			ISRC:          "CAA509814003",
			RecordingMBID: "remix-mbid",
			Duration:      236,
		}},
	}}
}

var drinkingQuery = acqports.RecordingQuery{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: "CAA509814003"}

func TestResolve_ISRCRecordingReplacesMismatchedSearchMBID(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "single-edit", Duration: 220},
		{MBID: "album-version", Duration: 237},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "album-version" {
		t.Errorf("MBID = %q, want the ISRC recording nearest the search length", identity.MBID)
	}
	if identity.Duration != 237 {
		t.Errorf("Duration = %v, want the chosen recording's 237", identity.Duration)
	}
}

func TestResolve_SearchMBIDKeptWhenISRCAgrees(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "other", Duration: 236},
		{MBID: "remix-mbid", Duration: 300},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, _ := resolver.Resolve(context.Background(), drinkingQuery)
	if identity.MBID != "remix-mbid" || identity.Duration != 236 {
		t.Errorf("identity = %+v, want the search's MBID and length untouched", identity)
	}
}

func TestResolve_ISRCLookupFailureKeepsSearchIdentity(t *testing.T) {
	authority := &stubISRCAuthority{err: errors.New("503")}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil || identity.MBID != "remix-mbid" {
		t.Errorf("identity = %+v, err = %v; want the search identity and no error", identity, err)
	}
}

func TestResolve_NoISRCSkipsTheLookup(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "x"}}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	q := drinkingQuery
	q.ISRC = ""
	_, _ = resolver.Resolve(context.Background(), q)
	if authority.calls != 0 {
		t.Errorf("RecordingsByISRC called %d times without an ISRC", authority.calls)
	}
}

func TestResolve_ISRCAnchorsIdentityWhenSearchFails(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "album-version", Duration: 237}}}
	resolver := NewRecordingResolver(failingSearcher{}, WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "album-version" || identity.ISRC != "CAA509814003" || identity.Duration != 237 {
		t.Errorf("identity = %+v, want one built from the ISRC recording", identity)
	}
}

func TestResolve_SearchErrorSurfacesWithoutISRCAnswer(t *testing.T) {
	resolver := NewRecordingResolver(failingSearcher{}, WithISRCAuthority(&stubISRCAuthority{}))
	if _, err := resolver.Resolve(context.Background(), drinkingQuery); err == nil {
		t.Error("a search error with no ISRC recording to fall back on must surface")
	}
}

func TestResolve_ISRCAnchor_DurationEdges(t *testing.T) {
	cases := []struct {
		name         string
		searchLength int
		recordings   []discoveryports.ISRCRecording
		wantMBID     string
		wantDuration float64
	}{
		{
			name:       "no search length and several recordings keeps the search identity",
			recordings: []discoveryports.ISRCRecording{{MBID: "a", Duration: 236}, {MBID: "b", Duration: 237}},
			wantMBID:   "remix-mbid",
		},
		{
			name:         "no search length and one recording adopts it",
			recordings:   []discoveryports.ISRCRecording{{MBID: "only", Duration: 237}},
			wantMBID:     "only",
			wantDuration: 237,
		},
		{
			name:         "an exact tie keeps the first recording MusicBrainz lists",
			searchLength: 236,
			recordings:   []discoveryports.ISRCRecording{{MBID: "first", Duration: 235}, {MBID: "second", Duration: 237}},
			wantMBID:     "first",
			wantDuration: 235,
		},
		{
			name:         "recordings without a length are never the nearest",
			searchLength: 236,
			recordings:   []discoveryports.ISRCRecording{{MBID: "unknown"}, {MBID: "far", Duration: 300}},
			wantMBID:     "far",
			wantDuration: 300,
		},
		{
			name:         "several recordings none with a length keeps the search identity",
			searchLength: 236,
			recordings:   []discoveryports.ISRCRecording{{MBID: "x"}, {MBID: "y"}},
			wantMBID:     "remix-mbid",
			wantDuration: 236,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			search := drinkingInLA()
			search.out.Results[0].Duration = tc.searchLength
			resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{recordings: tc.recordings}))

			identity, err := resolver.Resolve(context.Background(), drinkingQuery)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if identity.MBID != tc.wantMBID || identity.Duration != tc.wantDuration {
				t.Errorf("identity = {MBID:%q Duration:%v}, want {MBID:%q Duration:%v}",
					identity.MBID, identity.Duration, tc.wantMBID, tc.wantDuration)
			}
		})
	}
}

func TestResolve_ISRCAnchorsIdentityWhenSearchFindsNothing(t *testing.T) {
	empty := stubSearcher{out: &search.SearchOutput{}}
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "album-version", Duration: 237}}}
	resolver := NewRecordingResolver(empty, WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil || identity.MBID != "album-version" || identity.ISRC != "CAA509814003" {
		t.Errorf("identity = %+v, err = %v; want one built from the lone ISRC recording", identity, err)
	}
}

func TestResolve_SoleISRCRecordingMismatchDoubtsTheReference(t *testing.T) {
	logs := captureDefaultLog(t)
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "5d6efd30", Duration: 235}}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "5d6efd30" {
		t.Errorf("MBID = %q, want the sole ISRC recording", identity.MBID)
	}
	if got, want := identity.MBIDs, []string{"5d6efd30"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("MBIDs = %v, want %v", got, want)
	}
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: the search MBID was not registered to the ISRC")
	}
	if !strings.Contains(logs.String(), "acquisition.reference_doubted") {
		t.Errorf("expected acquisition.reference_doubted log, got:\n%s", logs.String())
	}
}

func TestResolve_ISRCReferenceSetKeepsAllRegisteredRecordingsWhenSearchMBIDAgrees(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "other", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
		{MBID: "third", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"remix-mbid", "other", "third"}
	if len(identity.MBIDs) != len(want) {
		t.Fatalf("MBIDs = %v, want %v", identity.MBIDs, want)
	}
	for i, mbid := range want {
		if identity.MBIDs[i] != mbid {
			t.Errorf("MBIDs[%d] = %q, want %q", i, identity.MBIDs[i], mbid)
		}
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: the search MBID is registered to the ISRC")
	}
}

func TestResolve_FailedISRCLookupYieldsSearchMBIDAsTheOnlyReference(t *testing.T) {
	authority := &stubISRCAuthority{err: errors.New("503")}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := identity.MBIDs, []string{"remix-mbid"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("MBIDs = %v, want %v", got, want)
	}
}

func TestResolve_NoISRCRecordingsYieldsEmptyMBIDsWhenSearchFindsNothing(t *testing.T) {
	empty := stubSearcher{out: &search.SearchOutput{}}
	resolver := NewRecordingResolver(empty, WithISRCAuthority(&stubISRCAuthority{}))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(identity.MBIDs) != 0 {
		t.Errorf("MBIDs = %v, want empty when there is no MBID to reference", identity.MBIDs)
	}
}

func TestResolve_ISRCReferenceSetKeepsEveryRegisteredRecording(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "b", Duration: 236},
		{MBID: "c", Duration: 236},
		{MBID: "d", Duration: 236},
		{MBID: "e", Duration: 236},
		{MBID: "f", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
		{MBID: "g", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid", "b", "c", "d", "e", "f", "g"})
}

func TestResolve_SearchDurationDisagreementDoubtsTheReferenceWithNoISRC(t *testing.T) {
	search := drinkingInLA()
	q := drinkingQuery
	q.ISRC = ""
	q.Duration = 300

	resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{}))
	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "remix-mbid" {
		t.Errorf("MBID = %q, want the search MBID kept as-is", identity.MBID)
	}
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: 236s search result vs a 300s track disagrees by more than max(5s, 3%)")
	}
}

func TestResolve_SearchDurationWithinToleranceIsNotDoubted(t *testing.T) {
	search := drinkingInLA()
	q := drinkingQuery
	q.ISRC = ""
	q.Duration = 238

	resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{}))
	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: 236s is within tolerance of a 238s track")
	}
}

func TestDurationDisagrees_BoundariesAndUnknownLengths(t *testing.T) {
	cases := []struct {
		name string
		want float64
		got  float64
		out  bool
	}{
		{"zero want means unknown, never disagrees", 0, 236, false},
		{"zero got means unknown, never disagrees", 236, 0, false},
		{"exactly at the floor tolerance is not a disagreement", 100, 105, false},
		{"one second past the floor tolerance disagrees", 100, 106.01, true},
		{"exactly at the percentage tolerance is not a disagreement", 1000, 970, false},
		{"one second past the percentage tolerance disagrees", 1000, 969, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationDisagrees(tc.want, tc.got); got != tc.out {
				t.Errorf("durationDisagrees(%v, %v) = %v, want %v", tc.want, tc.got, got, tc.out)
			}
		})
	}
}

func assertMBIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("MBIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MBIDs = %v, want %v", got, want)
		}
	}
}

func TestResolve_ReferenceDoubtedLogCarriesSearchAnchoredAndISRC(t *testing.T) {
	logs := captureDefaultLog(t)
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "5d6efd30", Duration: 235}}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	if _, err := resolver.Resolve(context.Background(), drinkingQuery); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "acquisition.reference_doubted") {
			line = l
		}
	}
	for _, want := range []string{"search_mbid=remix-mbid", "anchored_mbid=5d6efd30", "isrc=CAA509814003"} {
		if !strings.Contains(line, want) {
			t.Errorf("reference_doubted log %q lacks %q", line, want)
		}
	}
	if !strings.Contains(logs.String(), "acquisition.identity_anchored_to_isrc") {
		t.Errorf("expected acquisition.identity_anchored_to_isrc log kept, got:\n%s", logs.String())
	}
}

func TestResolve_AnchoredReferenceSetListsAnchorFirstAndDropsTheSearchMBID(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "single-edit", Duration: 220},
		{MBID: "album-version", Duration: 237},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"album-version", "single-edit"})
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: the search MBID was not registered to the ISRC")
	}
}

func TestResolve_AmbiguousAnchorStillDoubtsTheUnregisteredSearchMBID(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 0
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "a", Duration: 236},
		{MBID: "b", Duration: 237},
	}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid", "a", "b"})
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: remix-mbid is not registered to the ISRC")
	}
}

func TestResolve_ReferenceSetListsARepeatedISRCRecordingOnce(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "other", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
		{MBID: "other", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid", "other"})
}

func TestResolve_ISRCRecordingWithoutAnMBIDIsNotAReference(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
		{MBID: "other", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid", "other"})
}

func TestResolve_SearchLengthOffByMoreThanFiveSecondsIsLoggedNotDoubtedWhenISRCAgrees(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 100
	q := drinkingQuery
	q.Duration = 106
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 100}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "remix-mbid" {
		t.Errorf("MBID = %q, want the search MBID kept", identity.MBID)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid"})
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: the ISRC registers the reference MBID, so 100s vs 106s is only logged")
	}
	out := logs.String()
	for _, want := range []string{"acquisition.catalog_length_conflict", "catalog_seconds=106", "reference_seconds=100"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q, got:\n%s", want, out)
		}
	}
}

func TestResolve_SearchLengthExactlyFiveSecondsOffIsNotDoubted(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 100
	q := drinkingQuery
	q.ISRC = ""
	q.Duration = 105
	resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{}))

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: 5s off is within max(5s, 3%), not more than it")
	}
}

func TestResolve_SearchLengthWithinThreePercentOfALongTrackIsNotDoubted(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 600
	q := drinkingQuery
	q.ISRC = ""
	q.Duration = 612
	resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{}))

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: 12s off a 600s track is within 3% (18s)")
	}
}

func TestResolve_UnknownTrackLengthNeverDoubtsTheReference(t *testing.T) {
	q := drinkingQuery
	q.ISRC = ""
	q.Duration = 0
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(&stubISRCAuthority{}))

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: a track with no length cannot disagree")
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid"})
}

func TestResolve_ISRCAnchoredAfterSearchFailureReferencesTheISRCRecording(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "album-version", Duration: 237}}}
	resolver := NewRecordingResolver(failingSearcher{}, WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"album-version"})
}

func TestPickRecording_AnotherVersionIsNotTheSavedSong(t *testing.T) {
	acoustic := discoverydomain.SearchResult{Kind: discoverydomain.ResultKindTrack, Title: "Take On Me (2017 Acoustic)", Subtitle: "a-ha", ISRC: "DEUM71724939"}
	studio := discoverydomain.SearchResult{Kind: discoverydomain.ResultKindTrack, Title: "Take on Me", Subtitle: "a-ha", ISRC: "USWB19901214"}
	got, ok := pickRecording([]discoverydomain.SearchResult{acoustic, studio}, acqports.RecordingQuery{Title: "Take on Me", Artist: "a-ha"})
	if !ok || got.ISRC != studio.ISRC {
		t.Fatalf("picked %q (%s), want the studio recording", got.Title, got.ISRC)
	}
	if _, ok := pickRecording([]discoverydomain.SearchResult{acoustic}, acqports.RecordingQuery{Title: "Take on Me", Artist: "a-ha"}); ok {
		t.Fatal("picked the acoustic recording for a plain title")
	}
}

func TestResolve_ISRCAnchorPicksTheRecordingNearestTheCatalogLength(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 167
	recordings := []discoveryports.ISRCRecording{{MBID: "long", Duration: 167}, {MBID: "catalog", Duration: 139}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(&stubISRCAuthority{recordings: recordings}))
	query := drinkingQuery
	query.Duration = 139

	identity, err := resolver.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "catalog" {
		t.Errorf("MBID = %q, want the recording nearest the catalog length", identity.MBID)
	}
}

type stubRecordingLookup struct {
	facts map[string]acqports.RecordingFacts
	err   error
	asked []string
}

func (s *stubRecordingLookup) RecordingByMBID(_ context.Context, mbid string) (acqports.RecordingFacts, bool, error) {
	s.asked = append(s.asked, mbid)
	facts, found := s.facts[mbid]
	return facts, found, s.err
}

const (
	cureStudioMBID = "2b2af12b-48f1-444e-8591-cacb9f88f02a"
	cureLiveMBID   = "95ccd64c-5b38-43e0-827d-bb14b271372a"
)

var (
	cureStudioFacts = acqports.RecordingFacts{
		MBID: cureStudioMBID, Title: "Boys Don\u2019t Cry", Artists: []string{"The Cure"}, Duration: 157.306,
	}
	cureLiveFacts = acqports.RecordingFacts{
		MBID: cureLiveMBID, Title: "Boys Don\u2019t Cry", Artists: []string{"The Cure"}, Duration: 187,
		Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
	}
	pinnedCureQuery = acqports.RecordingQuery{Title: "Boys Don't Cry", Artist: "The Cure", Duration: 157.07, MBID: cureStudioMBID}
)

func searchRanking(mbid string, duration int, sources ...discoverydomain.SourceRef) stubSearcher {
	return stubSearcher{out: &search.SearchOutput{Results: []discoverydomain.SearchResult{{
		Kind: discoverydomain.ResultKindTrack, Title: "Boys Don't Cry", Subtitle: "The Cure",
		RecordingMBID: mbid, Duration: duration, Sources: sources,
	}}}}
}

func cureLookup() *stubRecordingLookup {
	return &stubRecordingLookup{facts: map[string]acqports.RecordingFacts{
		cureStudioMBID: cureStudioFacts, cureLiveMBID: cureLiveFacts,
	}}
}

func TestResolve_TheCatalogPinIsTheIdentity(t *testing.T) {
	logs := captureDefaultLog(t)
	liveSource := discoverydomain.SourceRef{Provider: discoverydomain.ProviderYouTube, ExternalID: "live-video"}
	resolver := NewRecordingResolver(searchRanking(cureLiveMBID, 187, liveSource), WithRecordingLookup(cureLookup()))

	identity, err := resolver.Resolve(context.Background(), pinnedCureQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if identity.MBID != cureStudioMBID || !slices.Equal(identity.MBIDs, []string{cureStudioMBID}) {
		t.Errorf("identity MBID %q MBIDs %v, want the pinned studio recording alone", identity.MBID, identity.MBIDs)
	}
	if identity.Duration != 157.306 || identity.ReferenceDoubted {
		t.Errorf("duration %v doubted %v, want the pinned length and a trusted reference", identity.Duration, identity.ReferenceDoubted)
	}
	if _, adopted := identity.SourceFor(acqports.ProviderYouTube); adopted {
		t.Error("the live recording's youtube source must not follow a rejected search pick")
	}
	for _, want := range []string{"acquisition.identity_pin_disagrees", "pinned_mbid=" + cureStudioMBID, "search_mbid=" + cureLiveMBID} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs.String())
		}
	}
}

func TestResolve_PinnedIdentityHoldsWhateverTheTextSearchDoes(t *testing.T) {
	tests := []struct {
		name     string
		searcher recordingSearcher
	}{
		{"search finds nothing", stubSearcher{out: &search.SearchOutput{}}},
		{"search fails", failingSearcher{}},
		{"search has no resolver wired", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewRecordingResolver(tt.searcher, WithRecordingLookup(cureLookup()))

			identity, err := resolver.Resolve(context.Background(), pinnedCureQuery)

			if err != nil || identity.MBID != cureStudioMBID || identity.Duration != 157.306 {
				t.Errorf("identity %+v, err %v; want the pinned recording and its length", identity, err)
			}
		})
	}
}

func TestResolve_SearchAgreeingWithThePinKeepsItsSourcesAndLogsNoDisagreement(t *testing.T) {
	logs := captureDefaultLog(t)
	studioSource := discoverydomain.SourceRef{Provider: discoverydomain.ProviderYouTube, ExternalID: "studio-video"}
	resolver := NewRecordingResolver(searchRanking(cureStudioMBID, 157, studioSource), WithRecordingLookup(cureLookup()))

	identity, _ := resolver.Resolve(context.Background(), pinnedCureQuery)

	if got, ok := identity.SourceFor(acqports.ProviderYouTube); !ok || got.ExternalID != "studio-video" {
		t.Errorf("youtube source = %+v, %v; want the agreeing search's video", got, ok)
	}
	if strings.Contains(logs.String(), "identity_pin_disagrees") {
		t.Errorf("agreeing search logged a disagreement:\n%s", logs.String())
	}
}

func TestResolve_PinnedRecordingWithOneISRCAnchorsTheReferenceSet(t *testing.T) {
	facts := cureStudioFacts
	facts.ISRCs = []string{"GBAAA8000001"}
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "greatest-hits-sibling", Duration: 157}, {MBID: cureStudioMBID, Duration: 157},
	}}
	lookup := &stubRecordingLookup{facts: map[string]acqports.RecordingFacts{cureStudioMBID: facts}}
	resolver := NewRecordingResolver(searchRanking(cureLiveMBID, 187), WithRecordingLookup(lookup), WithISRCAuthority(authority))

	identity, _ := resolver.Resolve(context.Background(), pinnedCureQuery)

	if identity.ISRC != "GBAAA8000001" {
		t.Errorf("ISRC = %q, want the pinned recording's only ISRC", identity.ISRC)
	}
	if want := []string{cureStudioMBID, "greatest-hits-sibling"}; !slices.Equal(identity.MBIDs, want) {
		t.Errorf("MBIDs = %v, want the pin first then its ISRC siblings %v", identity.MBIDs, want)
	}
}

func TestResolve_PinnedRecordingWithSeveralISRCsLeavesTheISRCEmpty(t *testing.T) {
	facts := cureStudioFacts
	facts.ISRCs = []string{"GBAAA8000001", "GBAAA8000002"}
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "other"}}}
	lookup := &stubRecordingLookup{facts: map[string]acqports.RecordingFacts{cureStudioMBID: facts}}
	resolver := NewRecordingResolver(stubSearcher{out: &search.SearchOutput{}}, WithRecordingLookup(lookup), WithISRCAuthority(authority))

	identity, _ := resolver.Resolve(context.Background(), pinnedCureQuery)

	if identity.ISRC != "" || !slices.Equal(identity.MBIDs, []string{cureStudioMBID}) || authority.calls != 0 {
		t.Errorf("identity %+v isrc lookups %d; want no ISRC claimed and the pin alone", identity, authority.calls)
	}
}

func TestResolve_PinSurvivesAnUnknownOrUnreachablePinnedRecording(t *testing.T) {
	tests := []struct {
		name   string
		lookup acqports.RecordingLookup
		logged string
	}{
		{"musicbrainz does not know the mbid", &stubRecordingLookup{}, "acquisition.pinned_recording_unknown"},
		{"musicbrainz fails", &stubRecordingLookup{err: errors.New("503")}, "acquisition.pinned_recording_lookup_failed"},
		{"no lookup wired", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureDefaultLog(t)
			resolver := NewRecordingResolver(searchRanking(cureLiveMBID, 187), WithRecordingLookup(tt.lookup))

			identity, err := resolver.Resolve(context.Background(), pinnedCureQuery)

			if err != nil || identity.MBID != cureStudioMBID || !slices.Equal(identity.MBIDs, []string{cureStudioMBID}) {
				t.Errorf("identity %+v, err %v; want the bare pin", identity, err)
			}
			if identity.Duration != 0 || identity.ReferenceDoubted {
				t.Errorf("duration %v doubted %v; the search's live length must not leak in", identity.Duration, identity.ReferenceDoubted)
			}
			if !strings.Contains(logs.String(), tt.logged) {
				t.Errorf("log lacks %q:\n%s", tt.logged, logs.String())
			}
		})
	}
}

func TestResolver_RecordingByMBIDServesTheWiredLookup(t *testing.T) {
	resolver := NewRecordingResolver(nil, WithRecordingLookup(cureLookup()))

	facts, found, err := resolver.RecordingByMBID(context.Background(), cureLiveMBID)
	if err != nil || !found || facts.Disambiguation != cureLiveFacts.Disambiguation {
		t.Errorf("facts %+v found %v err %v; want the live recording's facts", facts, found, err)
	}
	if _, found, _ := NewRecordingResolver(nil).RecordingByMBID(context.Background(), cureLiveMBID); found {
		t.Error("a resolver without a lookup must report not found")
	}
}

func TestResolve_UnpinnedQueryNeverAsksTheRecordingLookup(t *testing.T) {
	lookup := cureLookup()
	resolver := NewRecordingResolver(searchRanking(cureLiveMBID, 187), WithRecordingLookup(lookup))
	query := pinnedCureQuery
	query.MBID = ""

	identity, _ := resolver.Resolve(context.Background(), query)

	if identity.MBID != cureLiveMBID || len(lookup.asked) != 0 {
		t.Errorf("identity %+v lookups %v; want the search pick and no lookup without a pin", identity, lookup.asked)
	}
}

type stubRecordingAuthority struct {
	recording discoveryports.MBRecording
	found     bool
	err       error
}

func (s stubRecordingAuthority) RecordingByMBID(context.Context, string) (discoveryports.MBRecording, bool, error) {
	return s.recording, s.found, s.err
}

func TestNewRecordingLookup_CarriesEveryFactAcrossTheDiscoveryBoundary(t *testing.T) {
	authority := stubRecordingAuthority{found: true, recording: discoveryports.MBRecording{
		MBID: cureLiveMBID, Title: "Boys Don’t Cry", Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
		Artists: []string{"The Cure"}, ISRCs: []string{"GBAAA8000001"}, Duration: 187,
	}}

	facts, found, err := NewRecordingLookup(authority).RecordingByMBID(context.Background(), cureLiveMBID)

	want := acqports.RecordingFacts{
		MBID: cureLiveMBID, Title: "Boys Don’t Cry", Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
		Artists: []string{"The Cure"}, ISRCs: []string{"GBAAA8000001"}, Duration: 187,
	}
	if err != nil || !found || !reflect.DeepEqual(facts, want) {
		t.Errorf("facts %+v found %v err %v; want %+v", facts, found, err, want)
	}
}

func TestNewRecordingLookup_PassesNotFoundAndFailureThrough(t *testing.T) {
	boom := errors.New("503")

	_, found, err := NewRecordingLookup(stubRecordingAuthority{}).RecordingByMBID(context.Background(), cureLiveMBID)
	if found || err != nil {
		t.Errorf("unknown mbid = found %v err %v; want not found without error", found, err)
	}
	if _, _, err := NewRecordingLookup(stubRecordingAuthority{err: boom}).RecordingByMBID(context.Background(), cureLiveMBID); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the authority's failure", err)
	}
}

func TestResolve_CatalogLengthConflictWithISRCRegisteredReferenceIsNotDoubted(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 300
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 197}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: the ISRC registers the reference MBID")
	}
	if identity.MBID != "remix-mbid" || identity.Duration != 197 {
		t.Errorf("identity = %q/%vs, want remix-mbid/197s kept", identity.MBID, identity.Duration)
	}
	out := logs.String()
	for _, want := range []string{"acquisition.catalog_length_conflict", "catalog_seconds=300", "reference_seconds=197"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q, got:\n%s", want, out)
		}
	}
}

func TestResolve_CatalogLengthAgreeingWithISRCRegisteredReferenceLogsNoConflict(t *testing.T) {
	search := drinkingInLA()
	q := drinkingQuery
	q.Duration = 237
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 236}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	if _, err := resolver.Resolve(context.Background(), q); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("unexpected conflict log:\n%s", logs.String())
	}
}

func TestResolve_CatalogLengthConflictWithOtherISRCRecordingsStillDoubts(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 300
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "other-mbid", Duration: 300}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: the ISRC registers other recordings only")
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("unexpected conflict log:\n%s", logs.String())
	}
}

func TestResolve_CatalogLengthConflictStillDoubtsWhenTheISRCLookupFails(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 300
	authority := &stubISRCAuthority{err: errors.New("503")}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: a failed ISRC lookup leaves only the 300s vs 197s disagreement")
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("conflict log emitted without the ISRC registering the reference:\n%s", logs.String())
	}
}

func TestResolve_CatalogLengthConflictIsNotDoubtedWhenTheISRCRegistersTheReferenceAmongSeveral(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 300
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "other-mbid", Duration: 300},
		{MBID: "remix-mbid", Duration: 197},
		{MBID: "third-mbid", Duration: 250},
	}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: the ISRC registers the reference MBID, even among other recordings")
	}
	if identity.MBID != "remix-mbid" || identity.Duration != 197 {
		t.Errorf("identity = %q/%vs, want remix-mbid/197s kept, not the 300s recording nearest the catalog length", identity.MBID, identity.Duration)
	}
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "acquisition.catalog_length_conflict") {
			line = l
		}
	}
	for _, want := range []string{"isrc=CAA509814003", "mbid=remix-mbid", "catalog_seconds=300", "reference_seconds=197"} {
		if !strings.Contains(line, want) {
			t.Errorf("catalog_length_conflict log %q lacks %q", line, want)
		}
	}
}

func TestResolve_UnknownCatalogLengthWithISRCRegisteredReferenceLogsNoConflict(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 0
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 197}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: an unknown catalog length cannot disagree")
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("conflict logged for an unknown catalog length:\n%s", logs.String())
	}
}

func TestResolve_UnknownReferenceLengthWithISRCRegisteredReferenceLogsNoConflict(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 0
	q := drinkingQuery
	q.Duration = 300
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 0}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: an unknown reference length cannot disagree")
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("conflict logged for an unknown reference length:\n%s", logs.String())
	}
}

func TestResolve_CatalogLengthExactlyFiveSecondsOffISRCRegisteredReferenceLogsNoConflict(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 197
	q := drinkingQuery
	q.Duration = 202
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 197}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))
	logs := captureDefaultLog(t)

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = true, want false: 5s off is within max(5s, 3%)")
	}
	if strings.Contains(logs.String(), "acquisition.catalog_length_conflict") {
		t.Errorf("conflict logged for lengths within tolerance:\n%s", logs.String())
	}
}

func TestResolve_PinnedReferenceIsNotDoubtedByAWrongCatalogLength(t *testing.T) {
	q := pinnedCureQuery
	q.Duration = 300
	resolver := NewRecordingResolver(searchRanking(cureStudioMBID, 157), WithRecordingLookup(cureLookup()))

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != cureStudioMBID || identity.ReferenceDoubted {
		t.Errorf("identity %q doubted %v, want the pinned recording, never doubted", identity.MBID, identity.ReferenceDoubted)
	}
}
