package rank

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func threeProviderArtist(name string) domain.SearchResult {
	card := discoverytest.Res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
	return multiSource(multiSource(card, domain.ProviderSpotify), domain.ProviderAppleMusic)
}

func sameTitleTracks(title string, popularity float64) []Entity {
	var out []Entity
	for _, by := range []string{"Augustana", "Other Band", "Third Act"} {
		out = append(out, ent(discoverytest.WithPop(discoverytest.Track(title, by, domain.ProviderDeezer, nil), popularity)))
	}
	return out
}

func TestRank_ArtistCardOnThreeProvidersOutranksSameTitleTracks(t *testing.T) {
	entities := append(sameTitleTracks("Boston", 900), ent(threeProviderArtist("Boston")))

	got := Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindArtist {
		t.Fatalf("want the three-provider Boston artist card first, got %v", discoverytest.Titles(got))
	}
}

func TestRank_ArtistCardOnTwoProvidersStaysBelowPopularTracks(t *testing.T) {
	weak := multiSource(discoverytest.Res(domain.ResultKindArtist, "Boston", "", domain.ProviderDeezer, nil), domain.ProviderSpotify)
	entities := append(sameTitleTracks("Boston", 900), ent(weak))

	got := Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("a two-provider card must not be lifted, got %v", discoverytest.Titles(got))
	}
}

func TestRank_ArtistCardWithDifferentNameIsNotLifted(t *testing.T) {
	entities := append(sameTitleTracks("Boston", 900), ent(threeProviderArtist("Boston Manor")))

	got := Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("only an exact-name artist is lifted, got %v", discoverytest.Titles(got))
	}
}
