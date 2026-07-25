package handler

import (
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"
)

func TestSearchResultToDTO_PrefersStampedSignature(t *testing.T) {
	preFill := discdomain.ResultSignature(discdomain.SearchResult{
		Kind:  discdomain.ResultKindArtist,
		Title: "Nas",
	})
	sr := discdomain.SearchResult{
		Kind:      discdomain.ResultKindArtist,
		Title:     "Nas",
		Subtitle:  "American rapper",
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
