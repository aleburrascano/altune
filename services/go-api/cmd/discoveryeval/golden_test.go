package main

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCommittedGoldens_AreASixtyCaseMixOfEveryKindAndTheNamedTraps(t *testing.T) {
	cases, err := loadGoldens()
	if err != nil {
		t.Fatalf("loadGoldens: %v", err)
	}

	kinds := map[string]int{}
	queries := []string{}
	for _, c := range cases {
		kinds[c.Kind]++
		queries = append(queries, c.Query)
	}
	if len(cases) < 60 || kinds["track"] < 20 || kinds["album"] < 10 || kinds["artist"] < 10 {
		t.Errorf("golden mix = %d cases %v, want at least 60 with plenty of every kind", len(cases), kinds)
	}
	for _, trap := range []string{"nine inch nails hurt", "johnny cash hurt", "taylor swift lover album", "weezer blue album", "weezer green album", "nirvana", "nirvana uk 60s band", "topic breaking me", "adele hello", "fearless (taylor's version)", "21 savage redrum sped up"} {
		if !slices.Contains(queries, trap) {
			t.Errorf("golden set lacks the trap query %q", trap)
		}
	}
}

func TestCommittedGoldens_EveryKnownFailingMarkerNamesATicket(t *testing.T) {
	cases, err := loadGoldens()
	if err != nil {
		t.Fatalf("loadGoldens: %v", err)
	}

	for _, c := range cases {
		if c.KnownFailing != "" && !strings.HasPrefix(c.KnownFailing, "#") {
			t.Errorf("case %q: known_failing %q is not a ticket number", c.Query, c.KnownFailing)
		}
	}
}

type rankerFromGoldens struct {
	byQuery map[string][]discovery.SearchResult
	err     error
}

func (r rankerFromGoldens) Search(_ context.Context, query string) ([]discovery.SearchResult, error) {
	return r.byQuery[query], r.err
}

func hitFor(c discovery.GoldenCase) discovery.SearchResult {
	kind, _ := discovery.ParseResultKind(c.Kind)
	r := discovery.SearchResult{Kind: kind, Title: c.Title, Subtitle: c.Artist}
	if len(c.ISRCs) > 0 {
		r.ISRC = c.ISRCs[0]
	}
	setMBID(&r, c.MBID)
	return r
}

func setMBID(r *discovery.SearchResult, id string) {
	switch r.Kind {
	case discovery.ResultKindArtist:
		r.ArtistMBID = id
	case discovery.ResultKindAlbum:
		r.ReleaseGroupMBID = id
	default:
		r.RecordingMBID = id
	}
}

func decoyFor(c discovery.GoldenCase) discovery.SearchResult {
	r := hitFor(c)
	r.Title += " (Live)"
	r.ISRC = "ZZZZZ0000000"
	setMBID(&r, "00000000-0000-0000-0000-000000000000")
	return r
}

func rankerFor(cases []discovery.GoldenCase, rank func(discovery.GoldenCase) []discovery.SearchResult) rankerFromGoldens {
	byQuery := map[string][]discovery.SearchResult{}
	for _, c := range cases {
		byQuery[c.Query] = rank(c)
	}
	return rankerFromGoldens{byQuery: byQuery}
}

func goldenRunOptions() options {
	return options{concurrency: 2, baselinesPath: "baselines.json"}
}

func TestRunGoldenWith_AGoodRankerPassesAgainstTheCommittedBaselines(t *testing.T) {
	cases, err := loadGoldens()
	if err != nil {
		t.Fatalf("loadGoldens: %v", err)
	}
	good := rankerFor(cases, func(c discovery.GoldenCase) []discovery.SearchResult {
		if c.KnownFailing != "" {
			return []discovery.SearchResult{decoyFor(c)}
		}
		return []discovery.SearchResult{hitFor(c)}
	})

	err = runGoldenWith(context.Background(), cases, good, func() {}, goldenRunOptions())
	if err != nil {
		t.Fatalf("runGoldenWith = %v, want a pass on a ranker that finds every case that is not known failing", err)
	}
}

func TestRunGoldenWith_ADeliberatelyBrokenRankerFailsTheRun(t *testing.T) {
	cases, err := loadGoldens()
	if err != nil {
		t.Fatalf("loadGoldens: %v", err)
	}
	brokenRankers := map[string]rankerFromGoldens{
		"buries every right answer": rankerFor(cases, func(c discovery.GoldenCase) []discovery.SearchResult {
			return []discovery.SearchResult{decoyFor(c), decoyFor(c), decoyFor(c), decoyFor(c), hitFor(c)}
		}),
		"returns nothing":       rankerFor(cases, func(discovery.GoldenCase) []discovery.SearchResult { return nil }),
		"errors on every query": {byQuery: map[string][]discovery.SearchResult{}, err: errors.New("provider down")},
	}

	for name, broken := range brokenRankers {
		t.Run(name, func(t *testing.T) {
			err := runGoldenWith(context.Background(), cases, broken, func() {}, goldenRunOptions())

			if !errors.Is(err, errRegressed) {
				t.Errorf("runGoldenWith = %v, want errRegressed", err)
			}
		})
	}
}

func TestRunGoldenWith_AFixedKnownFailingCaseFailsUntilItsMarkerIsRemoved(t *testing.T) {
	cases, err := loadGoldens()
	if err != nil {
		t.Fatalf("loadGoldens: %v", err)
	}
	if !slices.ContainsFunc(cases, func(c discovery.GoldenCase) bool { return c.KnownFailing != "" }) {
		t.Fatal("the committed goldens carry no known-failing case, so the open bugs are invisible to the eval")
	}
	perfect := rankerFor(cases, func(c discovery.GoldenCase) []discovery.SearchResult { return []discovery.SearchResult{hitFor(c)} })

	err = runGoldenWith(context.Background(), cases, perfect, func() {}, goldenRunOptions())

	if !errors.Is(err, errRegressed) {
		t.Errorf("runGoldenWith = %v, want errRegressed because a known-failing case now passes", err)
	}
}
