package service

import (
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func verifyGroup(provider domain.ProviderName, titles ...string) ReleaseGroup {
	rs := make([]domain.SearchResult, len(titles))
	for i, t := range titles {
		rs[i] = domain.SearchResult{
			Kind:    domain.ResultKindAlbum,
			Title:   t,
			Sources: []domain.SourceRef{{Provider: provider, ExternalID: "x"}},
		}
	}
	return ReleaseGroup{Releases: rs, IDVerified: true}
}

func numberedTitles(prefix string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("%s %d", prefix, i)
	}
	return out
}

func TestFilterGroupsByMBAnchor_dropsMisbridgedProvider(t *testing.T) {
	rapperTitles := numberedTitles("rap", 35)
	mb := normalizeTitleSet(rapperTitles)

	apple := verifyGroup(domain.ProviderAppleMusic, rapperTitles[:20]...)
	soulTitles := append(numberedTitles("soul", 63), rapperTitles[0], rapperTitles[1], rapperTitles[2])
	deezer := verifyGroup(domain.ProviderDeezer, soulTitles...)

	got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{apple, deezer})

	if len(got) != 1 {
		t.Fatalf("kept %d groups, want 1 (apple kept, mis-bridged soul deezer dropped)", len(got))
	}
	if got[0].Releases[0].Sources[0].Provider != domain.ProviderAppleMusic {
		t.Errorf("kept the wrong group: %v", got[0].Releases[0].Sources[0].Provider)
	}
}

func TestFilterGroupsByMBAnchor_keepsSmallMatchingProvider(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 30))
	sc := verifyGroup(domain.ProviderSoundCloud, "rap 0", "rap 1", "rap 2", "sc exclusive")
	got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{sc})
	if len(got) != 1 {
		t.Errorf("dropped a small provider whose catalogue matches MB: %d kept", len(got))
	}
}

func TestFilterGroupsByMBAnchor_tooFewTitlesKept(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 30))
	small := verifyGroup(domain.ProviderDeezer, "unknown a", "unknown b")
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{small}); len(got) != 1 {
		t.Errorf("dropped a group too small to judge (%d kept)", len(got))
	}
}

func TestFilterGroupsByMBAnchor_weakAnchorKeepsAll(t *testing.T) {
	mb := normalizeTitleSet([]string{"a", "b"})
	deezer := verifyGroup(domain.ProviderDeezer, numberedTitles("soul", 50)...)
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{deezer}); len(got) != 1 {
		t.Errorf("dropped a group despite MB being too weak to anchor (%d kept)", len(got))
	}
}
