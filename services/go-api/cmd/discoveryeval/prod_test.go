package main

import (
	"testing"
)

func timedSearch(query string, totalMs float64, cached bool, top ...map[string]any) searchEvent {
	payload := map[string]any{
		"fanout_ms": totalMs / 2, "identity_ms": 1.0, "rank_ms": 1.0, "artwork_ms": 1.0,
		"favorites_ms": 0.0, "related_ms": 0.0, "total_ms": totalMs,
		"cached": cached, "partial": false,
	}
	tops := make([]any, 0, len(top))
	for _, item := range top {
		tops = append(tops, item)
	}
	payload["top"] = tops
	return searchEvent{QueryNorm: query, Payload: payload}
}

func topItem(position float64, kind, title string) map[string]any {
	return map[string]any{"position": position, "kind": kind, "title": title}
}

func TestSummarizeSearches_stageTimesComeFromFreshSearchesOnly(t *testing.T) {
	events := []searchEvent{
		timedSearch("a", 100, false), timedSearch("b", 200, false), timedSearch("c", 1000, false),
		timedSearch("d", 2, true),
		{QueryNorm: "old", Payload: map[string]any{"result_count": 3.0}},
	}

	r := summarizeSearches(events, 0)

	if r.Searches != 5 || r.Timed != 4 || r.Fresh != 3 {
		t.Fatalf("searches/timed/fresh = %d/%d/%d, want 5/4/3", r.Searches, r.Timed, r.Fresh)
	}
	total := r.Stages[len(r.Stages)-1]
	if total.Name != "total_ms" || total.P50 != 200 || total.P95 != 1000 {
		t.Errorf("total = %+v, want p50 200 and p95 1000 over the 3 fresh searches", total)
	}
	if r.CachedShare != 0.25 {
		t.Errorf("cached share = %v, want 0.25 of timed searches", r.CachedShare)
	}
}

func TestSummarizeSearches_artistFirstCountsSearchesThatNamedAnArtist(t *testing.T) {
	events := []searchEvent{
		timedSearch("drake", 100, false, topItem(0, "artist", "Drake")),
		timedSearch("muse", 100, false, topItem(0, "track", "Uprising"), topItem(1, "artist", "Muse")),
		timedSearch("humble", 100, false, topItem(0, "track", "HUMBLE.")),
	}

	r := summarizeSearches(events, 1)

	if r.ArtistNamed != 2 || r.ArtistFirst != 1 {
		t.Errorf("artist named/first = %d/%d, want 2/1 (drake first, muse second, humble names no artist)", r.ArtistNamed, r.ArtistFirst)
	}
	if r.Abandoned != 1 {
		t.Errorf("abandoned = %d, want 1", r.Abandoned)
	}
}
