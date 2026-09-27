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

type stubSearcher struct {
	out *discoveryservice.SearchOutput
}

func (s stubSearcher) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*discoveryservice.SearchOutput, error) {
	return s.out, nil
}

func TestProviderKey_IsByteIdenticalToDiscoveryString(t *testing.T) {
	for p := discoverydomain.ProviderUnknown; p <= discoverydomain.ProviderSpotify; p++ {
		if got, want := providerKey(p), p.String(); got != want {
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

	for key, wantID := range map[string]string{
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

// drinkingInLA is the merged result that failed acquisition on staging: the
// album version's ISRC and length glued to the MBID of the "(Who Mix?)" remix.
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
