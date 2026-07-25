package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestResultKindToVocabKind(t *testing.T) {
	cases := []struct {
		in   domain.ResultKind
		want domain.VocabularyKind
	}{
		{domain.ResultKindArtist, domain.VocabKindArtist},
		{domain.ResultKindTrack, domain.VocabKindTrack},
		{domain.ResultKindAlbum, domain.VocabKindAlbum},
		{domain.ResultKindUnknown, domain.VocabKindQuery},
		{domain.ResultKindPlaylist, domain.VocabKindQuery},
	}
	for _, c := range cases {
		if got := resultKindToVocabKind(c.in); got != c.want {
			t.Errorf("resultKindToVocabKind(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildVocabEntries_MapsResultsToEntries(t *testing.T) {
	results := []domain.SearchResult{
		withPop(res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil), 90),
		withPop(track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), 80),
		withPop(res(domain.ResultKindAlbum, "Scorpion", "Drake", domain.ProviderDeezer, nil), 70),
	}

	entries := buildVocabEntries("Drake!", results)

	if len(entries) != 5 {
		t.Fatalf("want 5 entries, got %d: %+v", len(entries), entries)
	}

	q := entries[0]
	if q.Kind != domain.VocabKindQuery || q.Term != "Drake!" || q.TermNorm != "drake" {
		t.Errorf("query entry: got %+v", q)
	}

	artist := entries[1]
	if artist.Kind != domain.VocabKindArtist || artist.Term != "Drake" || artist.TermNorm != "drake" || artist.Popularity != 90 {
		t.Errorf("artist entry: got %+v", artist)
	}

	tr := entries[2]
	if tr.Kind != domain.VocabKindTrack || tr.Term != "HUMBLE. - Kendrick Lamar" || tr.TermNorm != "humble kendrick lamar" || tr.Popularity != 80 {
		t.Errorf("track entry: got %+v", tr)
	}

	trArtist := entries[3]
	if trArtist.Kind != domain.VocabKindArtist || trArtist.Term != "Kendrick Lamar" || trArtist.Popularity != 80 {
		t.Errorf("track-subtitle artist entry: got %+v", trArtist)
	}

	al := entries[4]
	if al.Kind != domain.VocabKindAlbum || al.Term != "Scorpion - Drake" {
		t.Errorf("album entry: got %+v", al)
	}
}

func TestBuildVocabEntries_IngestsOnlyTopFive(t *testing.T) {
	var results []domain.SearchResult
	for _, name := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		results = append(results, res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil))
	}

	entries := buildVocabEntries("query", results)

	if len(entries) != 1+vocabIngestTop {
		t.Fatalf("want %d entries, got %d", 1+vocabIngestTop, len(entries))
	}
	last := entries[len(entries)-1]
	if last.Term != "E" {
		t.Errorf("want the 5th result last, got %q", last.Term)
	}
}

func TestBuildVocabEntries_FewerResultsThanTop(t *testing.T) {
	entries := buildVocabEntries("q", []domain.SearchResult{
		res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil),
	})
	if len(entries) != 2 {
		t.Fatalf("want query + 1 result entry, got %d", len(entries))
	}
}

func TestBuildVocabEntries_EmptyTitleIsNotFiltered(t *testing.T) {
	entries := buildVocabEntries("q", []domain.SearchResult{
		res(domain.ResultKindTrack, "", "", domain.ProviderDeezer, nil),
	})
	if len(entries) != 2 {
		t.Fatalf("want query + empty-title entry (current unfiltered behavior), got %d: %+v", len(entries), entries)
	}
	if entries[1].Term != "" || entries[1].TermNorm != "" {
		t.Errorf("want the empty term ingested as-is, got %+v", entries[1])
	}
}
