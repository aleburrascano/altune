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

func TestSearchResultToDTO_ProjectsMetadataIntoExtras(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:         discdomain.ResultKindAlbum,
		Title:        "Illmatic",
		Subtitle:     "Nas",
		Album:        "Illmatic",
		ISRC:         "USIR19400001",
		UPC:          "074643991124",
		MBID:         "abc-123",
		Year:         1994,
		ReleaseDate:  "1994-04-19",
		TrackCount:   10,
		ProviderRank: 3,
		FanCount:     42,
		Extras:       map[string]any{"custom": "keep"},
		Sources: []discdomain.SourceRef{
			{Provider: discdomain.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/album/123"},
		},
	}

	dto := searchResultToDTO(sr)

	want := map[string]any{
		"custom":       "keep",
		"album":        "Illmatic",
		"isrc":         "USIR19400001",
		"upc":          "074643991124",
		"mbid":         "abc-123",
		"year":         1994,
		"release_date": "1994-04-19",
		"track_count":  10,
		"rank":         int64(3),
		"nb_fan":       int64(42),
	}
	if len(dto.Extras) != len(want) {
		t.Fatalf("Extras has %d keys, want %d: %#v", len(dto.Extras), len(want), dto.Extras)
	}
	for k, v := range want {
		if dto.Extras[k] != v {
			t.Errorf("Extras[%q] = %#v, want %#v", k, dto.Extras[k], v)
		}
	}
	if len(dto.Sources) != 1 || dto.Sources[0].Provider != "deezer" {
		t.Errorf("Sources = %#v, want one deezer source", dto.Sources)
	}
}

func TestSearchResultToDTO_ZeroValuedMetadataOmittedFromExtras(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:  discdomain.ResultKindArtist,
		Title: "Nas",
	}

	dto := searchResultToDTO(sr)

	for _, k := range []string{"album", "isrc", "upc", "mbid", "year", "release_date", "track_count", "rank", "nb_fan"} {
		if _, set := dto.Extras[k]; set {
			t.Errorf("Extras[%q] should be omitted for zero-valued field, got %#v", k, dto.Extras[k])
		}
	}
}
