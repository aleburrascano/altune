package goapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// adminDiscographyQualityBody is the pinned seam response (epic #1425): one
// artist's cross-provider structural-quality case.
const adminDiscographyQualityBody = `{
	"window_days": 30,
	"group_by": "artist",
	"suspect_rate": 0.375,
	"last_sample_at": "2026-09-16T08:30:00Z",
	"cases": [
		{
			"artist": "Radiohead",
			"artist_ref": "spotify:4Z8W4fKeB5YxbusRsdQVPb",
			"releases": 42,
			"single_provider": 9,
			"single_provider_no_id": 4,
			"provider_counts": {"spotify": 40, "musicbrainz": 12},
			"last_seen": "2026-09-15T12:00:00Z"
		}
	]
}`

// TestAdminDiscographyQualityDecodesStubbedResponse is the Done proof for the
// read: the client hits a stubbed /admin/quality/discography with a GET and
// decodes the pinned case list, including the per-provider split.
func TestAdminDiscographyQualityDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET (the read must never mutate)", r.Method)
		}
		if r.URL.Path != "/observe/quality/discography" {
			t.Errorf("stub got path %s, want /observe/quality/discography", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminDiscographyQualityBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background())
	if err != nil {
		t.Fatalf("AdminDiscographyQuality: unexpected error: %v", err)
	}
	if got.WindowDays != 30 || got.GroupBy != "artist" {
		t.Fatalf("window/group = %d/%q, want 30/artist", got.WindowDays, got.GroupBy)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(got.Cases))
	}
	c := got.Cases[0]
	if c.Artist != "Radiohead" || c.ArtistRef != "spotify:4Z8W4fKeB5YxbusRsdQVPb" {
		t.Errorf("artist = %q ref = %q", c.Artist, c.ArtistRef)
	}
	if c.Releases != 42 || c.SingleProvider != 9 {
		t.Errorf("releases/single = %d/%d, want 42/9", c.Releases, c.SingleProvider)
	}
	if c.SingleProviderNoID != 4 {
		t.Errorf("single_provider_no_id = %d, want 4 (the id-backing evidence)", c.SingleProviderNoID)
	}
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Errorf("provider_counts = %v", c.ProviderCounts)
	}
	want := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	if !c.LastSeen.Equal(want) {
		t.Errorf("last_seen = %v, want %v", c.LastSeen, want)
	}
	if got.SuspectRate != 0.375 {
		t.Errorf("suspect_rate = %v, want 0.375 (the served windowed headline)", got.SuspectRate)
	}
	wantSample := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	if !got.LastSampleAt.Equal(wantSample) {
		t.Errorf("last_sample_at = %v, want %v", got.LastSampleAt, wantSample)
	}
}

// TestAdminDiscographyQualityToleratesVersionSkew proves the mirror ignores
// unknown fields a newer go-api adds rather than failing the read.
func TestAdminDiscographyQualityToleratesVersionSkew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"window_days":7,"group_by":"artist","trend":[1,2,3],"cases":[{"artist_ref":"deezer:1","releases":3,"single_provider":1,"provider_counts":{"deezer":3},"future_field":"x"}]}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background())
	if err != nil {
		t.Fatalf("AdminDiscographyQuality with extra fields: %v", err)
	}
	if got.WindowDays != 7 || len(got.Cases) != 1 || got.Cases[0].ArtistRef != "deezer:1" {
		t.Fatalf("decoded = %+v, want window 7 and one deezer case", got)
	}
}

// TestAdminDiscographyQualityRejectsNonOperator proves the read surfaces a
// non-operator/unauth rejection as an APIError rather than silently succeeding.
func TestAdminDiscographyQualityRejectsNonOperator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background()); err == nil {
		t.Fatal("AdminDiscographyQuality: want an error on a 403, got nil")
	}
}
