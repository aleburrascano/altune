package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeDiscographyReader is a controllable DiscographyQualityReader: it records
// the window and grouping it was asked for and returns a fixed case list.
type fakeDiscographyReader struct {
	gotSince        time.Time
	gotGroupBy      ports.DiscographyGroupBy
	gotLimit        int
	gotSuspectSince time.Time
	cases           []ports.DiscographyCase
	suspect         ports.DiscographySuspectRate
}

func (f *fakeDiscographyReader) DiscographyQuality(_ context.Context, since time.Time, groupBy ports.DiscographyGroupBy, limit int) ([]ports.DiscographyCase, error) {
	f.gotSince = since
	f.gotGroupBy = groupBy
	f.gotLimit = limit
	return f.cases, nil
}

func (f *fakeDiscographyReader) SuspectRate(_ context.Context, since time.Time) (ports.DiscographySuspectRate, error) {
	f.gotSuspectSince = since
	return f.suspect, nil
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
		ArtistRef:          "spotify:4Z8W4fKeB5YxbusRsdQVPb",
		Releases:           42,
		SingleProvider:     9,
		SingleProviderNoID: 3,
		ProviderCounts:     map[string]int{"spotify": 40, "musicbrainz": 12},
		LastSeen:           seen,
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
			Artist             string         `json:"artist"`
			ArtistRef          string         `json:"artist_ref"`
			Releases           int            `json:"releases"`
			SingleProvider     int            `json:"single_provider"`
			SingleProviderNoID int            `json:"single_provider_no_id"`
			ProviderCounts     map[string]int `json:"provider_counts"`
			LastSeen           time.Time      `json:"last_seen"`
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
	if c.SingleProviderNoID != 3 {
		t.Errorf("single_provider_no_id = %d, want 3", c.SingleProviderNoID)
	}
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Errorf("provider_counts = %v", c.ProviderCounts)
	}
	// The window asked of the store is ~30 days back.
	if d := time.Since(reader.gotSince); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("store window since = %v (%v ago), want ~30 days", reader.gotSince, d)
	}
}

// TestDiscographyQuality_ServesSuspectRate is the Done proof for the headline: the
// endpoint serves the reader's windowed suspect rate and its last-sample time on
// the wire, over the same window the cases are read for.
func TestDiscographyQuality_ServesSuspectRate(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	sampled := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{suspect: ports.DiscographySuspectRate{Rate: 0.25, LastSample: sampled}}
	r := mountQuality(operator, reader)

	req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography?window_days=14", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got struct {
		SuspectRate  float64   `json:"suspect_rate"`
		LastSampleAt time.Time `json:"last_sample_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.SuspectRate != 0.25 {
		t.Errorf("suspect_rate = %v, want 0.25", got.SuspectRate)
	}
	if !got.LastSampleAt.Equal(sampled) {
		t.Errorf("last_sample_at = %v, want %v", got.LastSampleAt, sampled)
	}
	// The rate is read over the same ~14-day window as the cases.
	if d := time.Since(reader.gotSuspectSince); d < 13*24*time.Hour || d > 15*24*time.Hour {
		t.Errorf("suspect-rate window since = %v (%v ago), want ~14 days", reader.gotSuspectSince, d)
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

// TestDiscographyQuality_GroupByParam proves the by= param is parsed to a known
// grouping, echoed as group_by, and passed to the reader — and that a hostile or
// unknown value (including an injection-shaped one) falls back to artist and is
// never reflected raw into the response or forwarded as-is.
func TestDiscographyQuality_GroupByParam(t *testing.T) {
	operator := shared.NewUserId(uuid.New())

	cases := []struct {
		raw  string
		want ports.DiscographyGroupBy
	}{
		{raw: "", want: ports.GroupByArtist},
		{raw: "artist", want: ports.GroupByArtist},
		{raw: "provider", want: ports.GroupByProvider},
		{raw: "contamination_band", want: ports.GroupByContaminationBand},
		{raw: "PROVIDER", want: ports.GroupByArtist},
		{raw: "artist'; DROP TABLE discovery_events;--", want: ports.GroupByArtist},
		{raw: "../../etc/passwd", want: ports.GroupByArtist},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			reader := &fakeDiscographyReader{}
			r := mountQuality(operator, reader)
			req := httptest.NewRequest(http.MethodGet, "/admin/quality/discography?by="+url.QueryEscape(tc.raw), nil)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			var got struct {
				GroupBy string `json:"group_by"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.GroupBy != string(tc.want) {
				t.Errorf("group_by = %q, want %q", got.GroupBy, tc.want)
			}
			if reader.gotGroupBy != tc.want {
				t.Errorf("reader got groupBy %q, want %q", reader.gotGroupBy, tc.want)
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
