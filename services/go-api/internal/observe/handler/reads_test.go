package handler

import (
	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/observe/evalmeter"
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
	status acqPorts.AcquisitionStatus
}

func (f fakeAcquisitionReader) Status() acqPorts.AcquisitionStatus { return f.status }

type fakeDiscographyReader struct {
	gotSince    time.Time
	gotGroupBy  ports.DiscographyGroupBy
	gotLimit    int
	gotDeadline time.Time
	cases       []ports.DiscographyCase
	suspect     ports.DiscographySuspectRate
}

func (f *fakeDiscographyReader) DiscographyQuality(ctx context.Context, since time.Time, groupBy ports.DiscographyGroupBy, limit int) ([]ports.DiscographyCase, error) {
	f.gotSince = since
	f.gotGroupBy = groupBy
	f.gotLimit = limit
	if deadline, ok := ctx.Deadline(); ok {
		f.gotDeadline = deadline
	}
	return f.cases, nil
}

func (f *fakeDiscographyReader) SuspectRate(_ context.Context, _ time.Time) (ports.DiscographySuspectRate, error) {
	return f.suspect, nil
}

func serveRead(t *testing.T, deps Deps, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	New(deps).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestReadsMetricsLive_ServesTheSource(t *testing.T) {
	rec := serveRead(t, Deps{LiveMetrics: func() LiveMetrics { return LiveMetrics{"latency": map[string]any{"routes": map[string]any{}}} }}, "/metrics/live")

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
	rec := serveRead(t, Deps{}, "/metrics/live")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{}\n" && got != "{}" {
		t.Errorf("body = %q, want an empty object", got)
	}
}

func TestReadsEval_ServesTheMeterStatus(t *testing.T) {
	rec := serveRead(t, Deps{Eval: evalmeter.New(true, 0, nil)}, "/eval")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got evalmeter.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Errorf("enabled = false, want true")
	}
}

func TestReadsEval_NilMeterAnswersDisabled(t *testing.T) {
	rec := serveRead(t, Deps{}, "/eval")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got evalmeter.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled || got.State != evalmeter.StateDisabled {
		t.Errorf("status = %+v, want disabled", got)
	}
}

func TestReadsAcquisition_ServesTheReaderStatus(t *testing.T) {
	scheduledAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	reader := fakeAcquisitionReader{status: acqPorts.AcquisitionStatus{
		InFlight:      2,
		Succeeded:     10,
		Failed:        3,
		Rejected:      7,
		QueueDepth:    5,
		QueueCapacity: 16,
		Verification:  acqPorts.AcquisitionVerification{Ffprobe: true, YtDlp: true},
		ActiveJobs: []acqPorts.JobRecord{{
			TrackID:     "trk-1",
			Title:       "Title One",
			State:       "running",
			ScheduledAt: scheduledAt,
		}},
	}}

	rec := serveRead(t, Deps{Acquisition: reader}, "/acquisition")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got acquisitionStatusDTO
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

func TestReadsAcquisition_NilReaderAnswers200WithEmptyStatus(t *testing.T) {
	rec := serveRead(t, Deps{}, "/acquisition")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got acquisitionStatusDTO
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

func TestReadsQuality_ServesTheWindowedCases(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []ports.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: ports.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}

	rec := serveRead(t, Deps{Discography: reader}, "/quality/discography?window_days=7&by=provider")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got discographyQualityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WindowDays != 7 || got.GroupBy != string(ports.GroupByProvider) {
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
	rec := serveRead(t, Deps{Discography: reader}, "/quality/discography")
	after := time.Now()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if reader.gotDeadline.IsZero() {
		t.Fatal("query ran with no deadline, want a bounded context")
	}
	minDeadline := before.Add(defaultQualityTimeout)
	maxDeadline := after.Add(defaultQualityTimeout)
	if reader.gotDeadline.Before(minDeadline) || reader.gotDeadline.After(maxDeadline) {
		t.Errorf("deadline = %v, want ~%v after the call (defaultQualityTimeout = %v)", reader.gotDeadline, defaultQualityTimeout, defaultQualityTimeout)
	}
}

func TestReadsQuality_NilProviderCountsBecomesAnEmptyObject(t *testing.T) {
	reader := &fakeDiscographyReader{cases: []ports.DiscographyCase{{ArtistRef: "spotify:artist"}}}
	rec := serveRead(t, Deps{Discography: reader}, "/quality/discography")

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
	rec := serveRead(t, Deps{}, "/quality/discography")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got discographyQualityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Cases) != 0 || got.WindowDays != defaultQualityWindowDays {
		t.Errorf("body = %+v, want empty cases and the default window", got)
	}
}

func TestReadsQuality_HostileWindowDaysIsClamped(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{raw: "999999999", want: maxQualityWindowDays},
		{raw: "-5", want: defaultQualityWindowDays},
		{raw: "0", want: defaultQualityWindowDays},
		{raw: "not-a-number", want: defaultQualityWindowDays},
		{raw: "1", want: 1},
		{raw: "365", want: 365},
		{raw: "366", want: maxQualityWindowDays},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			reader := &fakeDiscographyReader{}
			rec := serveRead(t, Deps{Discography: reader}, "/quality/discography?window_days="+tc.raw)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			var got discographyQualityResponse
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
	New(Deps{}).Register(r)

	for _, path := range []string{"/metrics/live", "/eval", "/acquisition", "/quality/discography"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", path, rec.Code)
		}
	}
}
