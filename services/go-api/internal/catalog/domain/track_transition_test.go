package domain

import (
	"errors"
	"reflect"
	"testing"
)

const transitionRef = "s3://bucket/track.opus"

type trackState func(t *testing.T) *Track

func pendingTrack(t *testing.T) *Track {
	t.Helper()
	return newTestTrack(t)
}

func readyTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkReady(transitionRef); err != nil {
		t.Fatalf("setup MarkReady: %v", err)
	}
	return track
}

func failedTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("setup MarkFailed: %v", err)
	}
	return track
}

// readyWithoutAudioTrack is a ready row missing its audio_ref, the broken
// state cmd/backfillaudio repairs with MarkReady.
func readyWithoutAudioTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	track.AcquisitionStatus = AcquisitionReady
	track.AcquisitionStartedAt = nil
	return track
}

var transitions = map[string]func(*Track) error{
	"MarkReady":        func(tr *Track) error { return tr.MarkReady("s3://bucket/other.opus") },
	"MarkReadySameRef": func(tr *Track) error { return tr.MarkReady(transitionRef) },
	"ReplaceAudio":     func(tr *Track) error { return tr.ReplaceAudio("s3://bucket/other.opus") },
	"MarkFailed":       func(tr *Track) error { return tr.MarkFailed("audio file missing from storage") },
	"FailAcquisition":  func(tr *Track) error { return tr.FailAcquisition("no_match_found") },
	"RevertToPending":  func(tr *Track) error { return tr.RevertToPending() },
}

func TestTrack_IllegalAcquisitionTransitionsAreRefusedAndLeaveTheTrackUnchanged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		from   trackState
		method string
	}{
		{"MarkReady on a ready track under a different ref", readyTrack, "MarkReady"},
		{"ReplaceAudio on a pending track", pendingTrack, "ReplaceAudio"},
		{"ReplaceAudio on a failed track", failedTrack, "ReplaceAudio"},
		{"MarkFailed on a failed track (duplicate failure)", failedTrack, "MarkFailed"},
		{"FailAcquisition on a ready track (stale failure after success)", readyTrack, "FailAcquisition"},
		{"FailAcquisition on a failed track", failedTrack, "FailAcquisition"},
		{"RevertToPending on a pending track", pendingTrack, "RevertToPending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			track := tc.from(t)
			before := *track

			err := transitions[tc.method](track)

			if !errors.Is(err, ErrIllegalAcquisitionTransition) {
				t.Fatalf("%s error = %v, want ErrIllegalAcquisitionTransition", tc.method, err)
			}
			if !reflect.DeepEqual(*track, before) {
				t.Errorf("track changed by a refused transition:\n got %+v\nwant %+v", *track, before)
			}
		})
	}
}

func TestTrack_LegalAcquisitionTransitionsSucceed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		from   trackState
		method string
		want   AcquisitionStatus
	}{
		{"MarkReady from pending (acquisition completed)", pendingTrack, "MarkReady", AcquisitionReady},
		{"MarkReady from failed (late success, backfill)", failedTrack, "MarkReady", AcquisitionReady},
		{"MarkReady on ready without audio (backfill repair)", readyWithoutAudioTrack, "MarkReady", AcquisitionReady},
		{"MarkReady on ready under the same ref (duplicate completion rewrote it)", readyTrack, "MarkReadySameRef", AcquisitionReady},
		{"ReplaceAudio on a ready track", readyTrack, "ReplaceAudio", AcquisitionReady},
		{"MarkFailed from pending (refused schedule, stale sweep)", pendingTrack, "MarkFailed", AcquisitionFailed},
		{"MarkFailed from ready (audio missing from storage)", readyTrack, "MarkFailed", AcquisitionFailed},
		{"FailAcquisition from pending", pendingTrack, "FailAcquisition", AcquisitionFailed},
		{"RevertToPending from ready (re-acquire)", readyTrack, "RevertToPending", AcquisitionPending},
		{"RevertToPending from failed (retry)", failedTrack, "RevertToPending", AcquisitionPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			track := tc.from(t)

			if err := transitions[tc.method](track); err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if track.AcquisitionStatus != tc.want {
				t.Errorf("status = %v, want %v", track.AcquisitionStatus, tc.want)
			}
		})
	}
}
