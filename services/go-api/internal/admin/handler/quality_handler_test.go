package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

// fakeDiscographyReader is a controllable DiscographyQualityReader: it records
// the window it was asked for and returns a fixed case list.
type fakeDiscographyReader struct {
	gotSince time.Time
	gotLimit int
	cases    []ports.DiscographyCase
}

func (f *fakeDiscographyReader) DiscographyQuality(_ context.Context, since time.Time, limit int) ([]ports.DiscographyCase, error) {
	f.gotSince = since
	f.gotLimit = limit
	return f.cases, nil
}

// mountQuality mounts the discography-quality route exactly as admin_wiring does:
// behind the operator gate, so the test exercises the same authority boundary
// production ships.
func mountQuality(operator shared.UserId, reader ports.DiscographyQualityReader) chi.Router {
	adminH := handler.New(nil, nil).WithDiscographyQuality(reader)
	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(handler.OperatorOnly(operator.String()))
			adminH.RegisterData(gr)
		})
	})
	return r
}

func TestDiscographyQuality_OperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())
	r := mountQuality(operator, &fakeDiscographyReader{})

	cases := []struct {
		name    string
		present bool
		user    shared.UserId
		want    int
	}{
		{name: "unauthenticated rejected", present: false, want: http.StatusUnauthorized},
		{name: "non-operator rejected", present: true, user: other, want: http.StatusForbidden},
		{name: "operator allowed", present: true, user: operator, want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography?by=artist&window_days=30", nil)
			if tc.present {
				req = req.WithContext(auth.ContextWithUserID(req.Context(), tc.user))
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestDiscographyQuality_ReturnsPinnedShape(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{cases: []ports.DiscographyCase{{
		ArtistRef:      "spotify:4Z8W4fKeB5YxbusRsdQVPb",
		Releases:       42,
		SingleProvider: 9,
		ProviderCounts: map[string]int{"spotify": 40, "musicbrainz": 12},
		LastSeen:       seen,
	}}}
	r := mountQuality(operator, reader)

	req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography?by=artist&window_days=30", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got struct {
		WindowDays int    `json:"window_days"`
		GroupBy    string `json:"group_by"`
		Cases      []struct {
			Artist         string         `json:"artist"`
			ArtistRef      string         `json:"artist_ref"`
			Releases       int            `json:"releases"`
			SingleProvider int            `json:"single_provider"`
			ProviderCounts map[string]int `json:"provider_counts"`
			LastSeen       time.Time      `json:"last_seen"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.WindowDays != 30 || got.GroupBy != "artist" {
		t.Fatalf("window_days=%d group_by=%q, want 30/artist", got.WindowDays, got.GroupBy)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(got.Cases))
	}
	c := got.Cases[0]
	if c.ArtistRef != "spotify:4Z8W4fKeB5YxbusRsdQVPb" || c.Releases != 42 || c.SingleProvider != 9 {
		t.Errorf("case fields wrong: %+v", c)
	}
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Errorf("provider_counts = %v", c.ProviderCounts)
	}
	// The window asked of the store is ~30 days back.
	if d := time.Since(reader.gotSince); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("store window since = %v (%v ago), want ~30 days", reader.gotSince, d)
	}
}

// TestDiscographyQuality_HostileWindowDays proves window_days is clamped, never
// trusted: a negative, huge, or garbage value falls back to a bounded window and
// never reflects raw attacker input into the response.
func TestDiscographyQuality_HostileWindowDays(t *testing.T) {
	operator := shared.NewUserId(uuid.New())

	cases := []struct {
		raw            string
		wantWindowDays int
	}{
		{raw: "", wantWindowDays: 30},
		{raw: "-5", wantWindowDays: 30},
		{raw: "0", wantWindowDays: 30},
		{raw: "not-a-number", wantWindowDays: 30},
		{raw: "999999999", wantWindowDays: 365},
		{raw: "7", wantWindowDays: 7},
		{raw: "<script>alert(1)</script>", wantWindowDays: 30},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			reader := &fakeDiscographyReader{}
			r := mountQuality(operator, reader)
			req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography?window_days="+tc.raw, nil)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got struct {
				WindowDays int `json:"window_days"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.WindowDays != tc.wantWindowDays {
				t.Errorf("window_days = %d, want %d", got.WindowDays, tc.wantWindowDays)
			}
		})
	}
}

// TestDiscographyQuality_UnconfiguredDegrades confirms an unconfigured reader
// answers an empty case list rather than failing the operator surface.
func TestDiscographyQuality_UnconfiguredDegrades(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	adminH := handler.New(nil, nil) // no WithDiscographyQuality
	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(handler.OperatorOnly(operator.String()))
			adminH.RegisterData(gr)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Cases []any `json:"cases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Cases) != 0 {
		t.Errorf("cases = %d, want 0 for an unconfigured reader", len(got.Cases))
	}
}
