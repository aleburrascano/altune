package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"math/rand"
	"testing"
)

type tier struct {
	name   string
	favour func(winner, loser *domain.Key)
}

var tiersInPrecedence = []tier{
	{"not demoted", func(_, l *domain.Key) { l.Demoted = true }},
	{"top pick", func(w, _ *domain.Key) { w.TopPick = true }},
	{"credits the picked artist", func(w, _ *domain.Key) { w.OfPick = true }},
	{"relevance", func(w, l *domain.Key) { w.Relevance, l.Relevance = 2, 1 }},
	{"named artist", func(w, _ *domain.Key) { w.NamedArtist = true }},
	{"prominence", func(w, l *domain.Key) { w.Prominence, l.Prominence = 2, 1 }},
	{"behavioral", func(w, l *domain.Key) { w.Behavioral, l.Behavioral = 2, 1 }},
	{"popularity", func(w, l *domain.Key) { w.Popularity, l.Popularity = 2, 1 }},
	{"multi-provider", func(w, _ *domain.Key) { w.MultiProvider = true }},
	{"reciprocal rank", func(w, l *domain.Key) { w.RRF, l.RRF = 2, 1 }},
	{"subtitle A to Z", func(w, l *domain.Key) { w.Subtitle, l.Subtitle = "a", "b" }},
	{"title A to Z", func(w, l *domain.Key) { w.Title, l.Title = "a", "b" }},
}

func TestLess_EachTierOutranksEveryTierBelowIt(t *testing.T) {
	for i, decisive := range tiersInPrecedence {
		t.Run(decisive.name, func(t *testing.T) {
			var a, b domain.Key
			decisive.favour(&a, &b)
			for _, lower := range tiersInPrecedence[i+1:] {
				lower.favour(&b, &a)
			}
			if !domain.Less(a, b) {
				t.Errorf("Less(a, b) = false, want a first on %s despite every lower tier favouring b:\na=%+v\nb=%+v", decisive.name, a, b)
			}
			if domain.Less(b, a) {
				t.Errorf("Less(b, a) = true, want b after a on %s:\na=%+v\nb=%+v", decisive.name, a, b)
			}
		})
	}
}

func TestLess_EqualKeysAreNotOrdered(t *testing.T) {
	k := domain.Key{Relevance: 1, TopPick: true, Demoted: true, Subtitle: "s", Title: "t"}
	if domain.Less(k, k) {
		t.Errorf("Less(k, k) = true, want false for %+v", k)
	}
	if domain.Less(domain.Key{}, domain.Key{}) {
		t.Error("Less of two zero keys = true, want false")
	}
}

func TestLess_ExperimentTierMatrix(t *testing.T) {
	tests := []struct {
		name string
		a, b domain.Key
	}{
		{"demotion overrides higher relevance", domain.Key{Relevance: 0.2}, domain.Key{Relevance: 1.0, Demoted: true}},
		{"both demoted fall through to relevance", domain.Key{Relevance: 1.0, Demoted: true}, domain.Key{Relevance: 0.5, Demoted: true}},
		{"relevance dominates prominence", domain.Key{Relevance: 1.0}, domain.Key{Relevance: 0.5, Prominence: 99}},
		{"prominence breaks a relevance tie before behavioral", domain.Key{Relevance: 1.0, Prominence: 5, Behavioral: -3}, domain.Key{Relevance: 1.0, Prominence: 1, Behavioral: 3}},
		{"equal prominence falls through to behavioral", domain.Key{Relevance: 1.0, Prominence: 5, Behavioral: 2}, domain.Key{Relevance: 1.0, Prominence: 5, Behavioral: -1}},
		{"behavioral dominates popularity", domain.Key{Relevance: 1.0, Behavioral: 1}, domain.Key{Relevance: 1.0, Popularity: 99}},
		{"demoted-but-prominent still loses to a plain result", domain.Key{Relevance: 0.1}, domain.Key{Relevance: 1.0, Prominence: 99, Behavioral: 9, Demoted: true}},
		{"higher relevance beats a behavioral boost", domain.Key{Relevance: 2.0, Behavioral: -5.0}, domain.Key{Relevance: 1.0, Behavioral: 5.0}},
		{"with no behavioral score popularity decides", domain.Key{Relevance: 1.0, Popularity: 5}, domain.Key{Relevance: 1.0, Popularity: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !domain.Less(tt.a, tt.b) {
				t.Errorf("Less(a, b) = false, want a first")
			}
			if domain.Less(tt.b, tt.a) {
				t.Error("both orderings true — not a strict weak ordering")
			}
		})
	}
}

func TestLess_StrictWeakOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pool := []float64{0, 1, 5, 10}
	pick := func() float64 { return pool[rng.Intn(len(pool))] }
	flip := func() bool { return rng.Intn(2) == 0 }
	randomKey := func() domain.Key {
		return domain.Key{
			Demoted: flip(), TopPick: flip(), OfPick: flip(), NamedArtist: flip(), MultiProvider: flip(),
			Relevance: pick(), Prominence: pick(), Behavioral: pick(), Popularity: pick(), RRF: pick(),
			Subtitle: string(rune('a' + rng.Intn(3))), Title: string(rune('a' + rng.Intn(3))),
		}
	}
	equiv := func(x, y domain.Key) bool { return !domain.Less(x, y) && !domain.Less(y, x) }
	for range 5000 {
		a, b, c := randomKey(), randomKey(), randomKey()
		if domain.Less(a, b) && domain.Less(b, a) {
			t.Fatalf("asymmetry violated:\na=%+v\nb=%+v", a, b)
		}
		if domain.Less(a, b) && domain.Less(b, c) && !domain.Less(a, c) {
			t.Fatalf("transitivity violated:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
		if equiv(a, b) && equiv(b, c) && !equiv(a, c) {
			t.Fatalf("incomparability not transitive:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
	}
}
