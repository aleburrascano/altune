package service

import (
	"math/rand"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func twoSourceTrack(title, artist string, rank int64) domain.SearchResult {
	r := track(title, artist, domain.ProviderITunes, nil)
	r.ProviderRank = rank
	r.Sources = append(r.Sources, domain.SourceRef{
		Provider: domain.ProviderMusicBrainz, ExternalID: title + ":mb", URL: "https://x/" + title,
	})
	return r
}

func artistWithFans(name string, nbFan int64) domain.SearchResult {
	r := res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
	r.FanCount = nbFan
	return r
}

func TestProminence_OffBuriesArtist_OnLiftsIt(t *testing.T) {
	artist := artistWithFans("Boston", 5_000_000)
	trk := twoSourceTrack("Boston", "Augustana", 50_000)
	entities := []Entity{ent(trk), ent(artist)}

	off := Rank(entities, "boston")
	if off[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence OFF: want track buried-state first, got %s %q", off[0].Kind, off[0].Title)
	}

	on := rankWith(entities, "boston", rankConfig{prominence: true})
	if on[0].Kind != domain.ResultKindArtist {
		t.Fatalf("prominence ON: want artist first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_ObscureArtistStaysBelowProminentTrack(t *testing.T) {
	artist := artistWithFans("FireWork", 30)
	trk := twoSourceTrack("Firework", "Katy Perry", 900_000)
	entities := []Entity{ent(artist), ent(trk)}

	on := rankWith(entities, "firework", rankConfig{prominence: true})
	if on[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence ON: want prominent track first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_SameKindEqualProminenceFallsThrough(t *testing.T) {
	hi := track("Echo", "Artist Hi", domain.ProviderDeezer, nil)
	hi.ProviderRank = 900_000
	lo := track("Echo", "Artist Lo", domain.ProviderDeezer, nil)
	lo.ProviderRank = 900_000
	entities := []Entity{ent(hi), ent(lo)}

	off := Rank(entities, "echo")
	on := rankWith(entities, "echo", rankConfig{prominence: true})

	if len(off) != len(on) {
		t.Fatalf("length changed: off %d on %d", len(off), len(on))
	}
	for i := range off {
		if off[i].Subtitle != on[i].Subtitle {
			t.Fatalf("same-kind order changed at %d: off %q on %q", i, off[i].Subtitle, on[i].Subtitle)
		}
	}
}

func TestRankLess_StrictWeakOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	kinds := []domain.ResultKind{domain.ResultKindArtist, domain.ResultKindAlbum, domain.ResultKindTrack}
	pool := []float64{0, 1, 5, 10}
	randScored := func() scored {
		return scored{
			result: domain.SearchResult{
				Kind:     kinds[rng.Intn(len(kinds))],
				Title:    string(rune('a' + rng.Intn(3))),
				Subtitle: string(rune('a' + rng.Intn(3))),
			},
			relevance:  pool[rng.Intn(len(pool))],
			behavioral: pool[rng.Intn(len(pool))],
			prominence: pool[rng.Intn(len(pool))],
			pop:        pool[rng.Intn(len(pool))],
			rrf:        pool[rng.Intn(len(pool))],
			multi:      rng.Intn(2) == 0,
			demoted:    rng.Intn(2) == 0,
		}
	}
	equiv := func(x, y scored) bool { return !rankLess(x, y) && !rankLess(y, x) }
	for i := 0; i < 5000; i++ {
		a, b, c := randScored(), randScored(), randScored()
		if rankLess(a, b) && rankLess(b, a) {
			t.Fatalf("asymmetry violated:\na=%+v\nb=%+v", a, b)
		}
		if rankLess(a, b) && rankLess(b, c) && !rankLess(a, c) {
			t.Fatalf("transitivity violated:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
		if equiv(a, b) && equiv(b, c) && !equiv(a, c) {
			t.Fatalf("incomparability not transitive:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
	}
}
