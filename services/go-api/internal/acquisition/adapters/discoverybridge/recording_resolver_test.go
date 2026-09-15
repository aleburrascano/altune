package discoverybridge

import (
	"altune/go-api/internal/shared"
	"context"
	"testing"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
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
