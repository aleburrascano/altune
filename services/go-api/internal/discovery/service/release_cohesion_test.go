package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func cohesionRelease(title string, providers ...domain.ProviderName) MergedRelease {
	ps := make(map[domain.ProviderName]bool, len(providers))
	for _, p := range providers {
		ps[p] = true
	}
	return MergedRelease{Result: domain.SearchResult{Title: title}, Providers: ps}
}

func hasTitle(rs []MergedRelease, title string) bool {
	for _, r := range rs {
		if r.Result.Title == title {
			return true
		}
	}
	return false
}

func TestFilterCohesive_dropsMisbridgedIsland(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("REST IN BASS", domain.ProviderAppleMusic, domain.ProviderSoundCloud, domain.ProviderMusicBrainz),
		cohesionRelease("Fully Loaded", domain.ProviderAppleMusic, domain.ProviderSoundCloud, domain.ProviderMusicBrainz),
		cohesionRelease("Baddest", domain.ProviderDeezer, domain.ProviderITunes),
		cohesionRelease("Ternobl", domain.ProviderDeezer),
		cohesionRelease("Por Siempre", domain.ProviderDeezer),
		cohesionRelease("Nafi", domain.ProviderDeezer),
	}

	got := FilterCohesive(releases)

	if !hasTitle(got, "REST IN BASS") || !hasTitle(got, "Fully Loaded") {
		t.Error("dropped a corroborated rapper release")
	}
	if hasTitle(got, "Ternobl") || hasTitle(got, "Por Siempre") || hasTitle(got, "Nafi") {
		t.Errorf("kept a mis-bridged soul-island single: %+v", got)
	}
}

func TestFilterCohesive_keepsCoreProviderExclusiveDropsIsland(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("Shared A", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("Shared B", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("SC Exclusive", domain.ProviderSoundCloud),
		cohesionRelease("Island", domain.ProviderLastFM),
	}
	got := FilterCohesive(releases)
	if !hasTitle(got, "SC Exclusive") {
		t.Error("dropped a real SC-exclusive though SoundCloud is in the core component")
	}
	if hasTitle(got, "Island") {
		t.Error("kept a disconnected island release")
	}
}

func TestFilterCohesive_equalComponentsTieBreakDeterministic(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("A1", domain.ProviderDeezer, domain.ProviderITunes),
		cohesionRelease("A2", domain.ProviderDeezer, domain.ProviderITunes),
		cohesionRelease("B1", domain.ProviderSoundCloud, domain.ProviderLastFM),
		cohesionRelease("B2", domain.ProviderSoundCloud, domain.ProviderLastFM),
	}
	for i := 0; i < 25; i++ {
		got := FilterCohesive(releases)
		if !hasTitle(got, "A1") || !hasTitle(got, "A2") || hasTitle(got, "B1") || hasTitle(got, "B2") {
			t.Fatalf("run %d: kept %+v, want the deezer/itunes component every run", i, got)
		}
	}
}

func TestFilterCohesive_noCorroborationKeepsAll(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("A", domain.ProviderDeezer),
		cohesionRelease("B", domain.ProviderDeezer),
		cohesionRelease("C", domain.ProviderDeezer),
	}
	if got := FilterCohesive(releases); len(got) != 3 {
		t.Errorf("kept %d, want 3 (no corroboration signal → keep all)", len(got))
	}
}

func TestFilterCohesive_singleSharedTitleDoesNotConnect(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("Collision", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("D only", domain.ProviderDeezer),
		cohesionRelease("SC only", domain.ProviderSoundCloud),
	}
	if got := FilterCohesive(releases); len(got) != 3 {
		t.Errorf("kept %d, want 3 (one shared title is below the ≥2 edge threshold → no fracture asserted)", len(got))
	}
}
