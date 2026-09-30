package discoverybridge

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryports "altune/go-api/internal/discovery/ports"
	discoveryservice "altune/go-api/internal/discovery/service"
)

import (
	"bytes"
	"log/slog"
	"strings"
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
	out *discoveryservice.SearchOutput
}

func (s stubSearcher) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*discoveryservice.SearchOutput, error) {
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
	resolver := NewRecordingResolver(stubSearcher{out: &discoveryservice.SearchOutput{
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

func (failingSearcher) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*discoveryservice.SearchOutput, error) {
	return nil, errors.New("search down")
}

func drinkingInLA() stubSearcher {
	return stubSearcher{out: &discoveryservice.SearchOutput{
		Results: []discoverydomain.SearchResult{{
			Kind:     discoverydomain.ResultKindTrack,
			Title:    "Drinking in L.A.",
			Subtitle: "Bran Van 3000",
			ISRC:     "CAA509814003",
			MBID:     "remix-mbid",
			Duration: 236,
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
	empty := stubSearcher{out: &discoveryservice.SearchOutput{}}
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
	empty := stubSearcher{out: &discoveryservice.SearchOutput{}}
	resolver := NewRecordingResolver(empty, WithISRCAuthority(&stubISRCAuthority{}))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(identity.MBIDs) != 0 {
		t.Errorf("MBIDs = %v, want empty when there is no MBID to reference", identity.MBIDs)
	}
}

func TestResolve_ISRCReferenceSetCapsAtFive(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "remix-mbid", Duration: 236},
		{MBID: "b", Duration: 236},
		{MBID: "c", Duration: 236},
		{MBID: "d", Duration: 236},
		{MBID: "e", Duration: 236},
		{MBID: "f", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(identity.MBIDs) != 5 {
		t.Errorf("MBIDs = %v, want exactly 5 (the cap)", identity.MBIDs)
	}
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

func TestResolve_CappedReferenceSetKeepsTheResolvedMBIDFirst(t *testing.T) {
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{
		{MBID: "b", Duration: 236},
		{MBID: "c", Duration: 236},
		{MBID: "d", Duration: 236},
		{MBID: "e", Duration: 236},
		{MBID: "f", Duration: 236},
		{MBID: "remix-mbid", Duration: 236},
	}}
	resolver := NewRecordingResolver(drinkingInLA(), WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), drinkingQuery)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid", "b", "c", "d", "e"})
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

func TestResolve_SearchLengthOffByMoreThanFiveSecondsDoubtsEvenWhenISRCAgrees(t *testing.T) {
	search := drinkingInLA()
	search.out.Results[0].Duration = 100
	q := drinkingQuery
	q.Duration = 106
	authority := &stubISRCAuthority{recordings: []discoveryports.ISRCRecording{{MBID: "remix-mbid", Duration: 100}}}
	resolver := NewRecordingResolver(search, WithISRCAuthority(authority))

	identity, err := resolver.Resolve(context.Background(), q)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.MBID != "remix-mbid" {
		t.Errorf("MBID = %q, want the search MBID kept", identity.MBID)
	}
	assertMBIDs(t, identity.MBIDs, []string{"remix-mbid"})
	if !identity.ReferenceDoubted {
		t.Error("ReferenceDoubted = false, want true: 100s vs 106s is 6s, past max(5s, 3%)")
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
