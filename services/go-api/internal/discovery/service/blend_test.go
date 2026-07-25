package service

import (
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func result(kind domain.ResultKind, title string) domain.SearchResult {
	return domain.SearchResult{Kind: kind, Title: title, Signature: title}
}

func TestBuildBlendedSlate_EmptyResults(t *testing.T) {
	slate := BuildBlendedSlate(nil, nil)

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

	slate := BuildBlendedSlate(results, results)

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

	slate := BuildBlendedSlate(results, results)

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
		results = append(results, result(domain.ResultKindTrack, fmt.Sprintf("track-%d", i)))
	}

	slate := BuildBlendedSlate(results, results)

	for _, section := range slate.Sections {
		if len(section.Items) > SectionCap {
			t.Errorf("section %s has %d items, want at most %d", section.Kind, len(section.Items), SectionCap)
		}
	}
}

func TestBuildBlendedSlate_SectionsSpanTheFullSlate(t *testing.T) {
	page := []domain.SearchResult{result(domain.ResultKindArtist, "top")}
	all := []domain.SearchResult{result(domain.ResultKindArtist, "top")}
	for i := 0; i < 6; i++ {
		all = append(all, result(domain.ResultKindTrack, fmt.Sprintf("beyond-page-%d", i)))
	}

	slate := BuildBlendedSlate(page, all)

	if len(slate.Sections) != 1 {
		t.Fatalf("Sections = %d, want 1", len(slate.Sections))
	}
	if len(slate.Sections[0].Items) != 6 {
		t.Errorf("section items = %d, want 6 drawn from beyond the first page", len(slate.Sections[0].Items))
	}
}

func TestBuildBlendedSlate_TopResultComesFromTheRenderedPage(t *testing.T) {
	page := []domain.SearchResult{result(domain.ResultKindTrack, "shuffled-to-front")}
	all := []domain.SearchResult{
		result(domain.ResultKindArtist, "ranked-first"),
		result(domain.ResultKindTrack, "shuffled-to-front"),
	}

	slate := BuildBlendedSlate(page, all)

	if slate.TopResult == nil || slate.TopResult.Title != "shuffled-to-front" {
		t.Fatalf("TopResult = %v, want the first item the user actually sees", slate.TopResult)
	}
	for _, section := range slate.Sections {
		for _, item := range section.Items {
			if item.Title == "shuffled-to-front" {
				t.Error("top result must not repeat inside its own section")
			}
		}
	}
}
