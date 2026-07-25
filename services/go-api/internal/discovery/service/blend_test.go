package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func result(kind domain.ResultKind, title string) domain.SearchResult {
	return domain.SearchResult{Kind: kind, Title: title}
}

func TestBuildBlendedSlate_EmptyResults(t *testing.T) {
	slate := BuildBlendedSlate(nil)

	if slate.TopResult != nil {
		t.Errorf("TopResult = %v, want nil", slate.TopResult)
	}
	if len(slate.Sections) != 0 {
		t.Errorf("Sections = %d, want 0", len(slate.Sections))
	}
}

func TestBuildBlendedSlate_TopResultExcludedFromItsSection(t *testing.T) {
	results := []domain.SearchResult{
		result(domain.ResultKindTrack, "top"),
		result(domain.ResultKindTrack, "second"),
	}

	slate := BuildBlendedSlate(results)

	if slate.TopResult == nil || slate.TopResult.Title != "top" {
		t.Fatalf("TopResult = %v, want title 'top'", slate.TopResult)
	}
	if len(slate.Sections) != 1 {
		t.Fatalf("Sections = %d, want 1", len(slate.Sections))
	}
	if len(slate.Sections[0].Items) != 1 || slate.Sections[0].Items[0].Title != "second" {
		t.Errorf("section items = %v, want only 'second'", slate.Sections[0].Items)
	}
}

func TestBuildBlendedSlate_SectionOrderFollowsStrongestMember(t *testing.T) {
	results := []domain.SearchResult{
		result(domain.ResultKindTrack, "t1"),
		result(domain.ResultKindArtist, "a1"),
		result(domain.ResultKindAlbum, "al1"),
		result(domain.ResultKindArtist, "a2"),
	}

	slate := BuildBlendedSlate(results)

	want := []domain.ResultKind{domain.ResultKindArtist, domain.ResultKindAlbum}
	if len(slate.Sections) != len(want) {
		t.Fatalf("Sections = %d, want %d (track section is empty after the top result)", len(slate.Sections), len(want))
	}
	for i, kind := range want {
		if slate.Sections[i].Kind != kind {
			t.Errorf("section[%d].Kind = %s, want %s", i, slate.Sections[i].Kind, kind)
		}
	}
}

func TestBuildBlendedSlate_CapsEachSection(t *testing.T) {
	results := []domain.SearchResult{result(domain.ResultKindArtist, "artist")}
	for i := 0; i < SectionCap+5; i++ {
		results = append(results, result(domain.ResultKindTrack, "track"))
	}

	slate := BuildBlendedSlate(results)

	for _, section := range slate.Sections {
		if len(section.Items) > SectionCap {
			t.Errorf("section %s has %d items, want at most %d", section.Kind, len(section.Items), SectionCap)
		}
	}
}
