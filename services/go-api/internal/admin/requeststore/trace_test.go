package requeststore

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

func TestRecordSearch_MergesWithExchangesUnderSameCorrID(t *testing.T) {
	s := New()
	// Transport captured a raw exchange first.
	s.recordExchange("c1", ex("{raw provider json}"))

	// Handler then records the search trace for the same correlation id.
	ctx := httputil.WithCorrelationID(t.Context(), "c1")
	statuses := []domain.ProviderSearchResponse{{
		Provider:    domain.ProviderDeezer,
		Status:      domain.ProviderStatusOK,
		LatencyMs:   88,
		ResultCount: 1,
		Results:     []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "Olympics"}},
	}}
	final := []domain.SearchResult{{
		Kind:    domain.ResultKindArtist,
		Title:   "Ken Carson",
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer}, {Provider: domain.ProviderITunes}},
	}}
	s.RecordSearch(ctx, "Ken Carson", []string{"artist", "track"}, "alex", statuses, final)

	rec, ok := s.Get("c1")
	if !ok {
		t.Fatal("record c1 not found")
	}
	if len(rec.Exchanges) != 1 {
		t.Errorf("exchange should be retained alongside the trace, got %d", len(rec.Exchanges))
	}
	if rec.Query != "Ken Carson" || rec.User != "alex" {
		t.Errorf("trace fields not merged: query=%q user=%q", rec.Query, rec.User)
	}
	if len(rec.Providers) != 1 || rec.Providers[0].Results[0].Title != "Olympics" {
		t.Errorf("provider results not projected: %+v", rec.Providers)
	}
	if len(rec.Final) != 1 || len(rec.Final[0].Sources) != 2 {
		t.Errorf("final projection wrong: %+v", rec.Final)
	}
}

func TestProjectResults_CarriesArtworkSourceAndPath(t *testing.T) {
	rows := ProjectResults([]domain.SearchResult{{
		Kind:          domain.ResultKindArtist,
		Title:         "Che",
		ImageURL:      "https://art/che.jpg",
		ArtworkSource: "discogs",
		Extras:        map[string]any{"artwork_path": "durable-identity"},
	}})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ArtworkSource != "discogs" {
		t.Errorf("artwork_source = %q, want discogs", rows[0].ArtworkSource)
	}
	if rows[0].ArtworkPath != "durable-identity" {
		t.Errorf("artwork_path = %q, want durable-identity", rows[0].ArtworkPath)
	}
}

func TestRecordSearch_NoCorrID_IsNoOp(t *testing.T) {
	s := New()
	s.RecordSearch(t.Context(), "q", nil, "u", nil, nil)
	if len(s.Snapshot()) != 0 {
		t.Error("RecordSearch without a correlation id must be a no-op")
	}
}
