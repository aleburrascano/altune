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

// The Che fracture (doc §7): MusicBrainz wrongly links the rapper's MBID to a
// different Che's Deezer id, so the fan-out returns two artists. The rapper's
// releases corroborate across Apple/SoundCloud/MusicBrainz; the mis-bridged soul
// Che's Deezer singles are an island that overlaps with nothing. Cohesion drops
// the island and keeps the corroborated core.
func TestFilterCohesive_dropsMisbridgedIsland(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("REST IN BASS", domain.ProviderAppleMusic, domain.ProviderSoundCloud, domain.ProviderMusicBrainz),
		cohesionRelease("Fully Loaded", domain.ProviderAppleMusic, domain.ProviderSoundCloud),
		cohesionRelease("Ternobl", domain.ProviderDeezer),     // soul island
		cohesionRelease("Por Siempre", domain.ProviderDeezer), // soul island
	}

	got := FilterCohesive(releases)

	if len(got) != 2 {
		t.Fatalf("kept %d, want 2 (rapper cluster; soul Deezer islands dropped): %+v", len(got), got)
	}
	if !hasTitle(got, "REST IN BASS") || !hasTitle(got, "Fully Loaded") {
		t.Error("dropped a corroborated rapper release")
	}
	if hasTitle(got, "Ternobl") || hasTitle(got, "Por Siempre") {
		t.Error("kept a mis-bridged soul island release")
	}
}

// A core provider's exclusive is kept: SoundCloud corroborates on one release, so
// it is in the core, so its SC-only release survives (a real SC-exclusive, not a
// mis-bridge).
func TestFilterCohesive_keepsCoreProviderExclusive(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("Shared Album", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("SC Exclusive", domain.ProviderSoundCloud), // SC is core → kept
	}
	got := FilterCohesive(releases)
	if len(got) != 2 || !hasTitle(got, "SC Exclusive") {
		t.Errorf("SC-exclusive dropped though SoundCloud is in the core: %+v", got)
	}
}

// No corroboration anywhere (a genuinely single-provider artist) → no signal to
// act on → keep everything, don't silently empty the discography.
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
