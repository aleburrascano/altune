package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func keepCase(providers int, idVerified, strongID bool) MergedRelease {
	ps := map[domain.ProviderName]bool{}
	names := []domain.ProviderName{domain.ProviderDeezer, domain.ProviderLastFM, domain.ProviderITunes}
	for i := 0; i < providers && i < len(names); i++ {
		ps[names[i]] = true
	}
	return MergedRelease{Providers: ps, IDVerified: idVerified, HasStrongID: strongID}
}

func TestKeepRelease(t *testing.T) {
	tests := []struct {
		name string
		m    MergedRelease
		want bool
	}{
		{"id-verified (ENCORE)", keepCase(1, true, false), true},
		{"name-fetched single source, no id (namesake)", keepCase(1, false, false), false},
		{"two by-name providers, no id-verified (still a namesake)", keepCase(2, false, false), false},
		{"strong id but not id-verified (namesake with own MBID)", keepCase(1, false, true), false},
		{"id-verified + by-name enrichment", keepCase(2, true, true), true},
	}
	for _, tt := range tests {
		if got := KeepRelease(tt.m); got != tt.want {
			t.Errorf("%s: KeepRelease = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFilterKept_dropsOnlyTheNamesake(t *testing.T) {
	releases := []MergedRelease{
		{Result: domain.SearchResult{Title: "Empty Clip"}, IDVerified: true, Providers: map[domain.ProviderName]bool{domain.ProviderDeezer: true}},
		{Result: domain.SearchResult{Title: "Wrong Che Single"}, Providers: map[domain.ProviderName]bool{domain.ProviderLastFM: true}},
		{Result: domain.SearchResult{Title: "REST IN BASS: ENCORE"}, IDVerified: true, Providers: map[domain.ProviderName]bool{domain.ProviderDeezer: true}},
	}
	kept := FilterKept(releases)
	if len(kept) != 2 {
		t.Fatalf("kept %d, want 2 (namesake dropped, both real releases kept)", len(kept))
	}
	for _, m := range kept {
		if m.Result.Title == "Wrong Che Single" {
			t.Error("namesake survived the filter")
		}
	}
}
