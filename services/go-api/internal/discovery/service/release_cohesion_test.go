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
// providers corroborate across MANY shared releases; the mis-bridged soul Che's
// Deezer is an island — even with ONE coincidental shared title ("Baddest", a
// same-name collision), it doesn't reach the ≥2 edge threshold, so it stays
// isolated and its singles drop.
func TestFilterCohesive_dropsMisbridgedIsland(t *testing.T) {
	releases := []MergedRelease{
		// Rapper cluster: apple/soundcloud/musicbrainz share ≥2 releases → connected.
		cohesionRelease("REST IN BASS", domain.ProviderAppleMusic, domain.ProviderSoundCloud, domain.ProviderMusicBrainz),
		cohesionRelease("Fully Loaded", domain.ProviderAppleMusic, domain.ProviderSoundCloud, domain.ProviderMusicBrainz),
		// Soul island: Deezer, plus a single coincidental title-collision with iTunes.
		cohesionRelease("Baddest", domain.ProviderDeezer, domain.ProviderITunes), // 1 shared → below threshold
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

// A core provider's exclusive is kept: SoundCloud shares ≥2 releases with the
// core, so it's in the component, so its SC-only release survives — while a
// disconnected island release is dropped.
func TestFilterCohesive_keepsCoreProviderExclusiveDropsIsland(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("Shared A", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("Shared B", domain.ProviderDeezer, domain.ProviderSoundCloud),
		cohesionRelease("SC Exclusive", domain.ProviderSoundCloud), // SC in component → kept
		cohesionRelease("Island", domain.ProviderLastFM),           // disconnected → dropped
	}
	got := FilterCohesive(releases)
	if !hasTitle(got, "SC Exclusive") {
		t.Error("dropped a real SC-exclusive though SoundCloud is in the core component")
	}
	if hasTitle(got, "Island") {
		t.Error("kept a disconnected island release")
	}
}

// A 2v2 fracture tie: two equal-size components must resolve the SAME way every
// request. The tie-break is the lexicographically smallest member name, so the
// {deezer, itunes} component always beats {lastfm, soundcloud}.
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

// No corroboration anywhere (single-provider artist) → no signal → keep all.
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

// A single shared title between two islands is not enough to fuse them (the
// collision guard): both stay below the edge threshold, so with no real
// multi-provider component everything is kept rather than falsely merged.
func TestFilterCohesive_singleSharedTitleDoesNotConnect(t *testing.T) {
	releases := []MergedRelease{
		cohesionRelease("Collision", domain.ProviderDeezer, domain.ProviderSoundCloud), // 1 shared
		cohesionRelease("D only", domain.ProviderDeezer),
		cohesionRelease("SC only", domain.ProviderSoundCloud),
	}
	if got := FilterCohesive(releases); len(got) != 3 {
		t.Errorf("kept %d, want 3 (one shared title is below the ≥2 edge threshold → no fracture asserted)", len(got))
	}
}
