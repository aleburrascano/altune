package handler_test

import (
	"altune/go-api/internal/platform/monitoring"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fakeAcquisitionReader struct {
	status monitoring.DownloadStatusReport
}

func (f fakeAcquisitionReader) Status() monitoring.DownloadStatusReport { return f.status }

type fakeDiscographyReader struct {
	gotSince    time.Time
	gotGroupBy  monitoring.DiscographyGroupBy
	gotLimit    int
	gotDeadline time.Time
	cases       []monitoring.DiscographyCase
	suspect     monitoring.DiscographySuspectRate
}

func (f *fakeDiscographyReader) DiscographyQuality(ctx context.Context, since time.Time, groupBy monitoring.DiscographyGroupBy, limit int) ([]monitoring.DiscographyCase, error) {
	f.gotSince = since
	f.gotGroupBy = groupBy
	f.gotLimit = limit
	if deadline, ok := ctx.Deadline(); ok {
		f.gotDeadline = deadline
	}
	return f.cases, nil
}

func (f *fakeDiscographyReader) SuspectRate(_ context.Context, _ time.Time) (monitoring.DiscographySuspectRate, error) {
	return f.suspect, nil
}

const (
	qualityLatestN           = 200
	qualityQueryBudget       = 5 * time.Second
	defaultQualityWindowDays = 30
	maxQualityWindowDays     = 365
)

type acquisitionJob struct {
	TrackID   string `json:"track_id"`
	SourceURL string `json:"source_url"`
}

type acquisitionStatus struct {
	InFlight      int              `json:"in_flight"`
	Succeeded     uint64           `json:"succeeded"`
	QueueCapacity int              `json:"queue_capacity"`
	ActiveJobs    []acquisitionJob `json:"jobs"`
	Recent        []acquisitionJob `json:"recent"`
}

type discographyCase struct {
	ArtistRef      string         `json:"artist_ref"`
	ProviderCounts map[string]int `json:"provider_counts"`
}

type discographyQuality struct {
	WindowDays  int               `json:"window_days"`
	GroupBy     string            `json:"group_by"`
	Cases       []discographyCase `json:"cases"`
	SuspectRate float64           `json:"suspect_rate"`
}

type evalStatus struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
}

func serveRead(t *testing.T, deps monitoring.Deps, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	monitoring.NewObserveHandler(deps).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestReadsMetricsLive_ServesTheSource(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{LiveMetrics: func() monitoring.LiveMetrics {
		return monitoring.LiveMetrics{"latency": map[string]any{"routes": map[string]any{}}}
	}}, "/metrics/live")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got["latency"]; !ok {
		t.Errorf("body %s lacks latency", rec.Body.String())
	}
}

func TestReadsMetricsLive_NilSourceAnswersEmptyObject(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{}, "/metrics/live")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{}\n" && got != "{}" {
		t.Errorf("body = %q, want an empty object", got)
	}
}

func TestReadsEval_ServesTheMeterStatus(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{Eval: monitoring.NewEvalMeter(0, nil)}, "/eval")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got evalStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Errorf("enabled = false, want true")
	}
}

func TestReadsEval_NilMeterAnswersDisabled(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{}, "/eval")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got evalStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled || got.State != "disabled" {
		t.Errorf("status = %+v, want disabled", got)
	}
}

func TestReadsAcquisition_ServesTheReaderStatus(t *testing.T) {
	scheduledAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	reader := fakeAcquisitionReader{status: monitoring.DownloadStatusReport{
		InFlight:      2,
		Succeeded:     10,
		Failed:        3,
		Rejected:      7,
		QueueDepth:    5,
		QueueCapacity: 16,
		Tools:         monitoring.AcquisitionTools{Ffprobe: true, YtDlp: true},
		ActiveJobs: []monitoring.AcquisitionJob{{
			TrackID:     "trk-1",
			Title:       "Title One",
			State:       "running",
			ScheduledAt: scheduledAt,
		}},
	}}

	rec := serveRead(t, monitoring.Deps{Acquisition: reader}, "/acquisition")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got acquisitionStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.InFlight != 2 || got.Succeeded != 10 || got.QueueCapacity != 16 {
		t.Errorf("status = %+v, want the reader's counters", got)
	}
	if len(got.ActiveJobs) != 1 || got.ActiveJobs[0].TrackID != "trk-1" {
		t.Errorf("active jobs = %+v, want the one job", got.ActiveJobs)
	}
}

func TestReadsAcquisition_ServesOldestPendingAgeAndDepthKnown(t *testing.T) {
	reader := fakeAcquisitionReader{status: monitoring.DownloadStatusReport{
		QueueDepth:       5,
		OldestPendingAge: 90 * time.Second,
		QueueDepthKnown:  true,
	}}

	rec := serveRead(t, monitoring.Deps{Acquisition: reader}, "/acquisition")

	body := decodeObject(t, rec.Body.Bytes())
	if got := string(body["oldest_pending_age_ms"]); got != "90000" {
		t.Errorf("oldest_pending_age_ms = %s, want 90000", got)
	}
	if got := string(body["queue_depth_known"]); got != "true" {
		t.Errorf("queue_depth_known = %s, want true", got)
	}
	if got := string(body["queue_depth"]); got != "5" {
		t.Errorf("queue_depth = %s, want 5", got)
	}
}

func TestReadsAcquisition_NilReaderAnswers200WithEmptyStatus(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{}, "/acquisition")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got acquisitionStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ActiveJobs == nil || len(got.ActiveJobs) != 0 {
		t.Errorf("active jobs = %+v, want an empty (non-nil) slice", got.ActiveJobs)
	}
	if got.Recent == nil || len(got.Recent) != 0 {
		t.Errorf("recent = %+v, want an empty (non-nil) slice", got.Recent)
	}
	if !strings.Contains(rec.Body.String(), `"jobs":[]`) || !strings.Contains(rec.Body.String(), `"recent":[]`) {
		t.Errorf("body = %s, want empty jobs/recent arrays, not null", rec.Body.String())
	}
}

func TestReadsQuality_AnUnknownGroupingIsABadRequestAndNeverReachesTheReader(t *testing.T) {
	reader := &fakeDiscographyReader{}

	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography?by=genre")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "observe.invalid_group_by") {
		t.Errorf("body = %s, want the observe.invalid_group_by code", rec.Body.String())
	}
	if reader.gotLimit != 0 {
		t.Errorf("the reader was asked for %d cases, want no call for a grouping nobody defined", reader.gotLimit)
	}
}

func TestReadsQuality_NoGroupingMeansByArtist(t *testing.T) {
	reader := &fakeDiscographyReader{}

	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography")

	if rec.Code != http.StatusOK || reader.gotGroupBy != monitoring.GroupByArtist {
		t.Fatalf("status = %d, grouping = %q, want 200 and %q", rec.Code, reader.gotGroupBy, monitoring.GroupByArtist)
	}
}

func TestReadsQuality_ServesTheWindowedCases(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []monitoring.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: monitoring.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}

	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography?window_days=7&by=provider")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got discographyQuality
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WindowDays != 7 || got.GroupBy != string(monitoring.GroupByProvider) {
		t.Errorf("window_days/group_by = %d/%q, want 7/provider", got.WindowDays, got.GroupBy)
	}
	if len(got.Cases) != 1 || got.Cases[0].ArtistRef != "spotify:artist" {
		t.Errorf("cases = %+v, want the one case", got.Cases)
	}
	if got.SuspectRate != 0.5 {
		t.Errorf("suspect_rate = %v, want 0.5", got.SuspectRate)
	}
	if reader.gotLimit != qualityLatestN {
		t.Errorf("limit = %d, want %d", reader.gotLimit, qualityLatestN)
	}
	if got.Cases[0].ProviderCounts["spotify"] != 4 {
		t.Errorf("provider_counts = %v, want spotify: 4", got.Cases[0].ProviderCounts)
	}
	if d := time.Since(reader.gotSince); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Errorf("store window since = %v (%v ago), want ~7 days", reader.gotSince, d)
	}
}

func TestReadsQuality_QueryDeadlineIsFiveSeconds(t *testing.T) {
	reader := &fakeDiscographyReader{}
	before := time.Now()
	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography")
	after := time.Now()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if reader.gotDeadline.IsZero() {
		t.Fatal("query ran with no deadline, want a bounded context")
	}
	minDeadline := before.Add(qualityQueryBudget)
	maxDeadline := after.Add(qualityQueryBudget)
	if reader.gotDeadline.Before(minDeadline) || reader.gotDeadline.After(maxDeadline) {
		t.Errorf("deadline = %v, want ~%v after the call", reader.gotDeadline, qualityQueryBudget)
	}
}

func TestReadsQuality_NilProviderCountsBecomesAnEmptyObject(t *testing.T) {
	reader := &fakeDiscographyReader{cases: []monitoring.DiscographyCase{{ArtistRef: "spotify:artist"}}}
	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeObject(t, rec.Body.Bytes())
	var cases []json.RawMessage
	if err := json.Unmarshal(body["cases"], &cases); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(cases))
	}
	case0 := decodeObject(t, cases[0])
	if string(case0["provider_counts"]) != "{}" {
		t.Errorf("provider_counts = %s, want an empty object, never null", case0["provider_counts"])
	}
}

func TestReadsQuality_NilReaderAnswersEmptyCases(t *testing.T) {
	rec := serveRead(t, monitoring.Deps{}, "/quality/discography")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got discographyQuality
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Cases) != 0 || got.WindowDays != defaultQualityWindowDays {
		t.Errorf("body = %+v, want empty cases and the default window", got)
	}
}

func TestReadsQuality_ABadWindowDaysIsA400ProblemNotADefault(t *testing.T) {
	for _, raw := range []string{"-5", "0", "not-a-number", "1.5", "%20"} {
		t.Run(raw, func(t *testing.T) {
			reader := &fakeDiscographyReader{}
			rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography?window_days="+raw)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"observe.invalid_window_days"`) {
				t.Errorf("body = %s, want code observe.invalid_window_days", rec.Body.String())
			}
			if !reader.gotSince.IsZero() {
				t.Errorf("reader was queried since %v, want no query for a rejected request", reader.gotSince)
			}
		})
	}
}

func TestReadsQuality_WindowDaysIsClampedToTheMaximum(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{raw: "999999999", want: maxQualityWindowDays},
		{raw: "1", want: 1},
		{raw: "365", want: 365},
		{raw: "366", want: maxQualityWindowDays},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			reader := &fakeDiscographyReader{}
			rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography?window_days="+tc.raw)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			var got discographyQuality
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.WindowDays != tc.want {
				t.Errorf("window_days = %d, want %d", got.WindowDays, tc.want)
			}
		})
	}
}

func TestReadsRoutes_NoPostRoute(t *testing.T) {
	r := chi.NewRouter()
	monitoring.NewObserveHandler(monitoring.Deps{}).Register(r)

	for _, path := range []string{"/metrics/live", "/eval", "/acquisition", "/quality/discography"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", path, rec.Code)
		}
	}
}

func TestReadsAcquisition_StripsCredentialsAndQueryFromSourceURL(t *testing.T) {
	scheduledAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	job := monitoring.AcquisitionJob{TrackID: "trk-1", State: "running", ScheduledAt: scheduledAt, SourceURL: "https://u:p@host.example/a/b?token=abc#f"}
	malformed := monitoring.AcquisitionJob{TrackID: "trk-2", State: "failed", ScheduledAt: scheduledAt, SourceURL: "http://[::1"}
	reader := fakeAcquisitionReader{status: monitoring.DownloadStatusReport{
		ActiveJobs: []monitoring.AcquisitionJob{job},
		Recent:     []monitoring.AcquisitionJob{job, malformed},
	}}

	rec := serveRead(t, monitoring.Deps{Acquisition: reader}, "/acquisition")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got acquisitionStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	const want = "https://host.example/a/b"
	if got.ActiveJobs[0].SourceURL != want || got.Recent[0].SourceURL != want {
		t.Errorf("source_url = %q / %q, want %q in jobs and recent", got.ActiveJobs[0].SourceURL, got.Recent[0].SourceURL, want)
	}
	if got.Recent[1].SourceURL != "" {
		t.Errorf("malformed source_url = %q, want empty", got.Recent[1].SourceURL)
	}
	if strings.Contains(rec.Body.String(), "token") || strings.Contains(rec.Body.String(), "u:p") {
		t.Errorf("body leaks credentials: %s", rec.Body.String())
	}
}
