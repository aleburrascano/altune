package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"testing"
)

func threeProviderArtist(name string) domain.SearchResult {
	card := memory.Res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
	return multiSource(multiSource(card, domain.ProviderSpotify), domain.ProviderAppleMusic)
}

func sameTitleTracks(title string, popularity float64) []service.Entity {
	var out []service.Entity
	for _, by := range []string{"Augustana", "Other Band", "Third Act"} {
		out = append(out, ent(memory.WithPop(memory.Track(title, by, domain.ProviderDeezer, nil), popularity)))
	}
	return out
}

func TestRank_ArtistCardOnThreeProvidersOutranksSameTitleTracks(t *testing.T) {
	entities := append(sameTitleTracks("Boston", 900), ent(threeProviderArtist("Boston")))

	got := service.Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindArtist {
		t.Fatalf("want the three-provider Boston artist card first, got %v", memory.Titles(got))
	}
}

func TestRank_ArtistCardOnTwoProvidersStaysBelowPopularTracks(t *testing.T) {
	weak := multiSource(memory.Res(domain.ResultKindArtist, "Boston", "", domain.ProviderDeezer, nil), domain.ProviderSpotify)
	entities := append(sameTitleTracks("Boston", 900), ent(weak))

	got := service.Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("a two-provider card must not be lifted, got %v", memory.Titles(got))
	}
}

func TestRank_ArtistCardWithDifferentNameIsNotLifted(t *testing.T) {
	entities := append(sameTitleTracks("Boston", 900), ent(threeProviderArtist("Boston Manor")))

	got := service.Rank(entities, "boston")

	if got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("only an exact-name artist is lifted, got %v", memory.Titles(got))
	}
}
