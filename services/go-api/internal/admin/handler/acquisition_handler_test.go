package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	acqService "altune/go-api/internal/acquisition/service"
)

type fakeAcquisitionReader struct {
	status acqService.AcquisitionStatus
}

func (f fakeAcquisitionReader) Status() acqService.AcquisitionStatus {
	return f.status
}

func serveAcquisitionBody(t *testing.T, h *AdminHandler) string {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterData(r)

	req := httptest.NewRequest(http.MethodGet, "/acquisition", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	return rec.Body.String()
}

func TestAcquisitionEndpoint_WireJSON_Populated(t *testing.T) {
	scheduledAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	reader := fakeAcquisitionReader{status: acqService.AcquisitionStatus{
		InFlight:     2,
		Succeeded:    10,
		Failed:       3,
		Verification: acqService.AcquisitionVerification{Ffprobe: true, Ffmpeg: false, Fpcalc: true},
		ActiveJobs: []acqService.JobRecord{{
			TrackID:        "trk-1",
			Title:          "Title One",
			Artist:         "Artist One",
			Album:          "Album One",
			SourceURL:      "https://src.example/one",
			ResolvedSource: "https://resolved.example/one",
			State:          "running",
			Stage:          "download",
			ScheduledAt:    scheduledAt,
			ElapsedMs:      1234,
			Reason:         "some reason",
			Provenance:     "corroborated",
		}},
		Recent: []acqService.JobRecord{{
			TrackID:     "trk-2",
			State:       "succeeded",
			ScheduledAt: scheduledAt,
			ElapsedMs:   5000,
		}},
	}}

	h := New(nil, nil).WithAcquisition(reader)

	want := `{"in_flight":2,"succeeded":10,"failed":3,"verification":{"ffprobe":true,"ffmpeg":false,"fpcalc":true},"jobs":[{"track_id":"trk-1","title":"Title One","artist":"Artist One","album":"Album One","source_url":"https://src.example/one","source":"https://resolved.example/one","state":"running","stage":"download","scheduled_at":"2026-09-07T12:00:00Z","elapsed_ms":1234,"reason":"some reason","provenance":"corroborated"}],"recent":[{"track_id":"trk-2","state":"succeeded","scheduled_at":"2026-09-07T12:00:00Z","elapsed_ms":5000}]}` + "\n"

	if got := serveAcquisitionBody(t, h); got != want {
		t.Errorf("acquisition wire JSON drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestAcquisitionEndpoint_WireJSON_Empty(t *testing.T) {
	h := New(nil, nil)

	want := `{"in_flight":0,"succeeded":0,"failed":0,"verification":{"ffprobe":false,"ffmpeg":false,"fpcalc":false},"jobs":[],"recent":[]}` + "\n"

	if got := serveAcquisitionBody(t, h); got != want {
		t.Errorf("empty acquisition wire JSON drifted\n got: %s\nwant: %s", got, want)
	}
}
