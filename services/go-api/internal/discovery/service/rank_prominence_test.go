package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// twoSourceTrack is a track corroborated by two providers (multi-source true),
// carrying a Deezer rank prominence. Two sources make it win the multi-source
// tiebreak over a single-source artist — the exact shape that buries the artist
// on a bare-name query.
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
	// Bare-name query: a prominent artist vs a same-name multi-source track. Both
	// tie on relevance (exact single-token title). With prominence OFF the
	// multi-source track wins (the bug); with it ON the more-prominent artist wins.
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
	// The firework shape: an obscure same-name artist must NOT be lifted over a
	// prominent track. Prominence is symmetric — it orders by magnitude, not by
	// kind — so the famous track still wins even with the rung ON.
	artist := artistWithFans("FireWork", 30)
	trk := twoSourceTrack("Firework", "Katy Perry", 900_000)
	entities := []Entity{ent(artist), ent(trk)}

	on := rankWith(entities, "firework", rankConfig{prominence: true})
	if on[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence ON: want prominent track first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_SameKindOrderUntouched(t *testing.T) {
	// The reversion guard: prominence fires ONLY across kinds. Two tracks that tie
	// on relevance must keep the exact order the existing ladder gives them, ON or
	// OFF — so the bare-title track corpus the popularity attempt regressed cannot
	// move.
	hi := track("Echo", "Artist Hi", domain.ProviderDeezer, nil)
	hi.ProviderRank = 900_000
	lo := track("Echo", "Artist Lo", domain.ProviderDeezer, nil)
	lo.ProviderRank = 10
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
