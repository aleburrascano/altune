package monitoringfromdownload_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromdownload"
	"altune/go-api/internal/download"
	"altune/go-api/internal/platform/monitoring"
	"context"
	"reflect"
	"testing"
	"time"
)

type countingStatusSource struct {
	calls  int
	status func(call int) download.Status
}

func (s *countingStatusSource) Status(_ context.Context) download.Status {
	s.calls++
	return s.status(s.calls)
}

func fixedStatus(status download.Status) *countingStatusSource {
	return &countingStatusSource{status: func(int) download.Status { return status }}
}

func jobRecord(trackID string, scheduledAt time.Time) download.JobRecord {
	return download.JobRecord{
		TrackID:        trackID,
		Title:          trackID + " title",
		Artist:         trackID + " artist",
		Album:          trackID + " album",
		SourceURL:      "https://u:p@host.example/" + trackID + "?token=t",
		ResolvedSource: trackID + " source",
		State:          "running",
		Stage:          trackID + " stage",
		ScheduledAt:    scheduledAt,
		ElapsedMs:      1234,
		Reason:         trackID + " reason",
		Provenance:     trackID + " provenance",
		Run:            3,
		Confidence:     0.5,
	}
}

func observedJob(trackID string, scheduledAt time.Time) monitoring.AcquisitionJob {
	return monitoring.AcquisitionJob{
		TrackID:        trackID,
		Title:          trackID + " title",
		Artist:         trackID + " artist",
		Album:          trackID + " album",
		SourceURL:      "https://u:p@host.example/" + trackID + "?token=t",
		ResolvedSource: trackID + " source",
		State:          "running",
		Stage:          trackID + " stage",
		ScheduledAt:    scheduledAt,
		ElapsedMs:      1234,
		Reason:         trackID + " reason",
		Provenance:     trackID + " provenance",
	}
}

func TestAcquisitionReader_CarriesEveryFieldObserveShows(t *testing.T) {
	scheduledAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	source := fixedStatus(download.Status{
		InFlight:         1,
		Succeeded:        2,
		Failed:           3,
		Rejected:         4,
		VerifySkipped:    5,
		Paused:           true,
		QueueDepth:       6,
		OldestPendingAge: 7 * time.Second,
		QueueDepthKnown:  true,
		Workers:          8,
		Verification:     download.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true, FingerprintVerified: true},
		ActiveJobs:       []download.JobRecord{jobRecord("active", scheduledAt)},
		Recent:           []download.JobRecord{jobRecord("recent-1", scheduledAt), jobRecord("recent-2", scheduledAt)},
	})

	got := monitoringfromdownload.NewAcquisitionReader(source).Status(t.Context())

	want := monitoring.DownloadStatusReport{
		InFlight:         1,
		Succeeded:        2,
		Failed:           3,
		Rejected:         4,
		Paused:           true,
		QueueDepth:       6,
		QueueCapacity:    8,
		OldestPendingAge: 7 * time.Second,
		QueueDepthKnown:  true,
		Tools:            monitoring.AcquisitionTools{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true},
		ActiveJobs:       []monitoring.AcquisitionJob{observedJob("active", scheduledAt)},
		Recent:           []monitoring.AcquisitionJob{observedJob("recent-1", scheduledAt), observedJob("recent-2", scheduledAt)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestAcquisitionReader_EachToolIsCarriedOnItsOwn(t *testing.T) {
	cases := map[string]struct {
		verification download.Verification
		want         monitoring.AcquisitionTools
	}{
		"ffprobe": {download.Verification{Ffprobe: true}, monitoring.AcquisitionTools{Ffprobe: true}},
		"ffmpeg":  {download.Verification{Ffmpeg: true}, monitoring.AcquisitionTools{Ffmpeg: true}},
		"fpcalc":  {download.Verification{Fpcalc: true}, monitoring.AcquisitionTools{Fpcalc: true}},
		"yt-dlp":  {download.Verification{YtDlp: true}, monitoring.AcquisitionTools{YtDlp: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := monitoringfromdownload.NewAcquisitionReader(fixedStatus(download.Status{Verification: tc.verification})).Status(t.Context())
			if got.Tools != tc.want {
				t.Fatalf("tools = %+v, want %+v", got.Tools, tc.want)
			}
		})
	}
}

func TestAcquisitionReader_NoJobsGiveEmptyListsNeverNil(t *testing.T) {
	got := monitoringfromdownload.NewAcquisitionReader(fixedStatus(download.Status{})).Status(t.Context())

	if got.ActiveJobs == nil || len(got.ActiveJobs) != 0 || got.Recent == nil || len(got.Recent) != 0 {
		t.Fatalf("jobs %#v recent %#v, want empty non-nil lists", got.ActiveJobs, got.Recent)
	}
}

func TestAcquisitionReader_EveryReadAsksTheSourceAgain(t *testing.T) {
	source := &countingStatusSource{status: func(call int) download.Status {
		return download.Status{InFlight: call}
	}}
	reader := monitoringfromdownload.NewAcquisitionReader(source)

	first, second := reader.Status(t.Context()), reader.Status(t.Context())

	if first.InFlight != 1 || second.InFlight != 2 {
		t.Fatalf("in flight = %d then %d, want 1 then 2", first.InFlight, second.InFlight)
	}
}
