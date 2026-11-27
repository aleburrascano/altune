package memory_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"testing"
)

func TestNewResult_ADefaultResultCarriesOnlyItsKindAndTitle(t *testing.T) {
	got := memory.NewResult(domain.ResultKindTrack, "Humble")

	if got.Kind != domain.ResultKindTrack || got.Title != "Humble" {
		t.Errorf("result = %+v, want a track titled Humble", got)
	}
	if got.Subtitle != "" || len(got.Sources) != 0 || got.Signature != "" || got.Extras != nil {
		t.Errorf("result = %+v, want no other field set", got)
	}
}

func TestNewResult_OptionsSetTheirFieldsAndSourcesAccumulate(t *testing.T) {
	extras := map[string]any{"popularity": 70}

	got := memory.NewResult(domain.ResultKindAlbum, "DAMN.",
		memory.WithSubtitle("Kendrick Lamar"),
		memory.WithSource(domain.ProviderDeezer, "d1", "https://deezer.com/d1"),
		memory.WithSource(domain.ProviderSpotify, "s1", ""),
		memory.WithConfidence(domain.ConfidenceLow),
		memory.WithSignature("damn"),
		memory.WithExtras(extras),
	)

	if got.Subtitle != "Kendrick Lamar" || got.Signature != "damn" || got.Confidence != domain.ConfidenceLow {
		t.Errorf("result = %+v, want the subtitle, signature and confidence set", got)
	}
	if len(got.Sources) != 2 || got.Sources[0].ExternalID != "d1" || got.Sources[0].URL != "https://deezer.com/d1" || got.Sources[1].Provider != domain.ProviderSpotify {
		t.Errorf("sources = %+v, want both sources in the order given", got.Sources)
	}
	if got.Extras["popularity"] != 70 {
		t.Errorf("extras = %v, want the map given", got.Extras)
	}
}
