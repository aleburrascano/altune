package handler_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type stubAcquisitionReader struct {
	calls  int
	status func(call int) monitoring.DownloadStatusReport
}

func (s *stubAcquisitionReader) Status(_ context.Context) monitoring.DownloadStatusReport {
	s.calls++
	return s.status(s.calls)
}

func serveAcquisition(t *testing.T, reader monitoring.AcquisitionReader) map[string]json.RawMessage {
	t.Helper()
	r := chi.NewRouter()
	monitoring.NewObserveHandler(clock.Now, clock.NewTicker, monitoring.Deps{Acquisition: reader}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/acquisition", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return body
}

func TestAcquisitionStatus_OneJobGivesOneEntryAndAnEmptyRecentList(t *testing.T) {
	r := &stubAcquisitionReader{status: func(int) monitoring.DownloadStatusReport {
		return monitoring.DownloadStatusReport{ActiveJobs: []monitoring.AcquisitionJob{{TrackID: "t1", State: "running"}}}
	}}

	body := serveAcquisition(t, r)

	var jobs []struct {
		TrackID string `json:"track_id"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(body["jobs"], &jobs); err != nil {
		t.Fatalf("decode jobs %s: %v", body["jobs"], err)
	}
	if len(jobs) != 1 || jobs[0].TrackID != "t1" || jobs[0].State != "running" {
		t.Fatalf("jobs = %+v, want the one running job", jobs)
	}
	if got := string(body["recent"]); got != "[]" {
		t.Fatalf("recent = %s, want an empty list, never null", got)
	}
}

func TestAcquisitionStatus_EveryRequestReadsAFreshStatus(t *testing.T) {
	r := &stubAcquisitionReader{status: func(call int) monitoring.DownloadStatusReport {
		return monitoring.DownloadStatusReport{InFlight: call}
	}}

	if got := string(serveAcquisition(t, r)["in_flight"]); got != "1" {
		t.Fatalf("first in_flight = %s, want 1", got)
	}
	if got := string(serveAcquisition(t, r)["in_flight"]); got != "2" {
		t.Fatalf("second in_flight = %s, want 2", got)
	}
}

func TestAcquisitionStatus_VerificationNamesOnlyLiveTools(t *testing.T) {
	r := &stubAcquisitionReader{status: func(int) monitoring.DownloadStatusReport {
		return monitoring.DownloadStatusReport{Tools: monitoring.AcquisitionTools{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}}
	}}

	raw := serveAcquisition(t, r)["verification"]

	var keys map[string]bool
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("decode verification %s: %v", raw, err)
	}
	want := map[string]bool{"ffprobe": true, "ffmpeg": true, "fpcalc": true, "yt_dlp": true}
	if len(keys) != len(want) {
		t.Fatalf("verification = %s, want exactly %v", raw, want)
	}
	for k, v := range want {
		if keys[k] != v {
			t.Fatalf("verification = %s, want %s=%v", raw, k, v)
		}
	}
}
