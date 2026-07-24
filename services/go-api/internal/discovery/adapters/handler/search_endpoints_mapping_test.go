package handler

import (
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"
)

// The wire signature must be the pre-disambiguation one the service stamped at
// rank time (domain.SearchResult.Signature) — recomputing after enrichment
// filled the artist subtitle would drift from the behavioral-ranking join key.
func TestSearchResultToDTO_PrefersStampedSignature(t *testing.T) {
	preFill := discdomain.ResultSignature(discdomain.SearchResult{
		Kind:  discdomain.ResultKindArtist,
		Title: "Nas",
	})
	sr := discdomain.SearchResult{
		Kind:      discdomain.ResultKindArtist,
		Title:     "Nas",
		Subtitle:  "American rapper", // filled by disambiguation AFTER the stamp
		Signature: preFill,
	}

	dto := searchResultToDTO(sr)

	if dto.ResultSignature != preFill {
		t.Errorf("ResultSignature = %q, want the stamped pre-fill %q", dto.ResultSignature, preFill)
	}
	if recomputed := discdomain.ResultSignature(sr); dto.ResultSignature == recomputed {
		t.Errorf("wire signature drifted to the post-fill recompute %q", recomputed)
	}
}

// Results that never went through mergeRankEnrich (no stamped Signature) keep
// the computed fallback.
func TestSearchResultToDTO_ComputesSignatureFallback(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:     discdomain.ResultKindTrack,
		Title:    "Hello",
		Subtitle: "Adele",
	}
	dto := searchResultToDTO(sr)
	if want := discdomain.ResultSignature(sr); dto.ResultSignature != want {
		t.Errorf("ResultSignature = %q, want computed fallback %q", dto.ResultSignature, want)
	}
}
