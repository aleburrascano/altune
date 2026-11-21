package observeacquisition_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/observeacquisition"
	"altune/go-api/internal/observe"
	"reflect"
	"testing"
	"time"
)

type countingStatusSource struct {
	calls  int
	status func(call int) acquisition.AcquisitionStatus
}

func (s *countingStatusSource) Status() acquisition.AcquisitionStatus {
	s.calls++
	return s.status(s.calls)
}

func fixedStatus(status acquisition.AcquisitionStatus) *countingStatusSource {
	return &countingStatusSource{status: func(int) acquisition.AcquisitionStatus { return status }}
}

func jobRecord(trackID string, scheduledAt time.Time) acquisition.JobRecord {
	return acquisition.JobRecord{
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

func observedJob(trackID string, scheduledAt time.Time) observe.AcquisitionJob {
	return observe.AcquisitionJob{
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
	source := fixedStatus(acquisition.AcquisitionStatus{
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
		Verification:     acquisition.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true, FingerprintVerified: true},
		ActiveJobs:       []acquisition.JobRecord{jobRecord("active", scheduledAt)},
		Recent:           []acquisition.JobRecord{jobRecord("recent-1", scheduledAt), jobRecord("recent-2", scheduledAt)},
	})

	got := observeacquisition.NewAcquisitionReader(source).Status()

	want := observe.AcquisitionStatus{
		InFlight:         1,
		Succeeded:        2,
		Failed:           3,
		Rejected:         4,
		Paused:           true,
		QueueDepth:       6,
		QueueCapacity:    8,
		OldestPendingAge: 7 * time.Second,
		QueueDepthKnown:  true,
		Tools:            observe.AcquisitionTools{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true},
		ActiveJobs:       []observe.AcquisitionJob{observedJob("active", scheduledAt)},
		Recent:           []observe.AcquisitionJob{observedJob("recent-1", scheduledAt), observedJob("recent-2", scheduledAt)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestAcquisitionReader_EachToolIsCarriedOnItsOwn(t *testing.T) {
	cases := map[string]struct {
		verification acquisition.AcquisitionVerification
		want         observe.AcquisitionTools
	}{
		"ffprobe": {acquisition.AcquisitionVerification{Ffprobe: true}, observe.AcquisitionTools{Ffprobe: true}},
		"ffmpeg":  {acquisition.AcquisitionVerification{Ffmpeg: true}, observe.AcquisitionTools{Ffmpeg: true}},
		"fpcalc":  {acquisition.AcquisitionVerification{Fpcalc: true}, observe.AcquisitionTools{Fpcalc: true}},
		"yt-dlp":  {acquisition.AcquisitionVerification{YtDlp: true}, observe.AcquisitionTools{YtDlp: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := observeacquisition.NewAcquisitionReader(fixedStatus(acquisition.AcquisitionStatus{Verification: tc.verification})).Status()
			if got.Tools != tc.want {
				t.Fatalf("tools = %+v, want %+v", got.Tools, tc.want)
			}
		})
	}
}

func TestAcquisitionReader_NoJobsGiveEmptyListsNeverNil(t *testing.T) {
	got := observeacquisition.NewAcquisitionReader(fixedStatus(acquisition.AcquisitionStatus{})).Status()

	if got.ActiveJobs == nil || len(got.ActiveJobs) != 0 || got.Recent == nil || len(got.Recent) != 0 {
		t.Fatalf("jobs %#v recent %#v, want empty non-nil lists", got.ActiveJobs, got.Recent)
	}
}

func TestAcquisitionReader_EveryReadAsksTheSourceAgain(t *testing.T) {
	source := &countingStatusSource{status: func(call int) acquisition.AcquisitionStatus {
		return acquisition.AcquisitionStatus{InFlight: call}
	}}
	reader := observeacquisition.NewAcquisitionReader(source)

	first, second := reader.Status(), reader.Status()

	if first.InFlight != 1 || second.InFlight != 2 {
		t.Fatalf("in flight = %d then %d, want 1 then 2", first.InFlight, second.InFlight)
	}
}
