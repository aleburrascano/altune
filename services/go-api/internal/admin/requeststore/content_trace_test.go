package requeststore

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

func TestRecordContentFetch_AttachesDetailWithYearAndStatus(t *testing.T) {
	s := New()
	ctx := httputil.WithCorrelationID(t.Context(), "c-detail")

	items := []domain.SearchResult{
		{Kind: domain.ResultKindAlbum, Title: "Newest", Extras: map[string]any{"year": 2024, "consensus_status": "confirmed"}},
		{Kind: domain.ResultKindAlbum, Title: "NoYear", Extras: map[string]any{"consensus_status": "unconfirmed"}},
	}
	s.RecordContentFetch(ctx, "albums", "deezer", "Che", "ok", items)

	rec, ok := s.Get("c-detail")
	if !ok || rec.Detail == nil {
		t.Fatal("expected a detail trace on the record")
	}
	if rec.Detail.Kind != "albums" || rec.Detail.Provider != "deezer" || rec.Detail.Artist != "Che" {
		t.Fatalf("detail header = %+v", rec.Detail)
	}
	if len(rec.Detail.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(rec.Detail.Items))
	}
	if rec.Detail.Items[0].Year != 2024 || rec.Detail.Items[0].Status != "confirmed" {
		t.Errorf("item 0 = %+v, want year 2024 / confirmed", rec.Detail.Items[0])
	}
	if rec.Detail.Items[1].Year != 0 {
		t.Errorf("item 1 year = %d, want 0 (unknown)", rec.Detail.Items[1].Year)
	}
}

func TestRecordContentFetch_NoCorrID_IsNoOp(t *testing.T) {
	s := New()
	s.RecordContentFetch(t.Context(), "albums", "deezer", "Che", "ok", nil)
	if len(s.Snapshot()) != 0 {
		t.Error("RecordContentFetch without a correlation id must be a no-op")
	}
}
