package domain_test

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"errors"
	"testing"
	"time"
)

func TestTransitionTable(t *testing.T) {
	t.Parallel()

	p, r, f := domain.AcquisitionPending, domain.AcquisitionReady, domain.AcquisitionFailed
	want := map[domain.AcquisitionEvent]map[domain.DownloadStatus]*domain.DownloadStatus{
		domain.EventMarkReady:       {p: &r, r: &r, f: &r},
		domain.EventReplaceAudio:    {p: nil, r: &r, f: nil},
		domain.EventMarkFailed:      {p: &f, r: &f, f: nil},
		domain.EventFailPending:     {p: &f, r: nil, f: nil},
		domain.EventRevertToPending: {p: nil, r: &p, f: &p},
	}
	if len(want) != len(domain.AcquisitionEvents()) {
		t.Fatalf("table covers %d events, the domain has %d", len(want), len(domain.AcquisitionEvents()))
	}
	for _, event := range domain.AcquisitionEvents() {
		for _, from := range domain.AcquisitionStatuses() {
			next, err := domain.Transition(from, event)
			legal := want[event][from]
			switch {
			case legal == nil && !errors.Is(err, domain.ErrIllegalAcquisitionTransition):
				t.Errorf("%s from %s: want illegal, got next=%s err=%v", event, from, next, err)
			case legal != nil && (err != nil || next != *legal):
				t.Errorf("%s from %s: want %s, got next=%s err=%v", event, from, *legal, next, err)
			}
		}
	}
}

func TestStatusSetsOwnTheQueueVocabulary(t *testing.T) {
	t.Parallel()

	if got := domain.ClaimableStatuses(); len(got) != 1 || got[0] != domain.AcquisitionPending {
		t.Errorf("claimable = %v, want [pending]", got)
	}
	if got := domain.RetryableStatuses(); len(got) != 1 || got[0] != domain.AcquisitionFailed {
		t.Errorf("retryable = %v, want [failed]", got)
	}
	if got := domain.UpgradableStatuses(); len(got) != 1 || got[0] != domain.AcquisitionReady {
		t.Errorf("upgradable = %v, want [ready]", got)
	}
}

func TestTrackMethodsAgreeWithTransition(t *testing.T) {
	t.Parallel()

	now := clock.Func(func() time.Time { return time.Unix(1_700_000_000, 0) })
	for _, from := range domain.AcquisitionStatuses() {
		for _, event := range domain.AcquisitionEvents() {
			track := trackIn(t, from)
			_, wantErr := domain.Transition(from, event)

			var err error
			switch event {
			case domain.EventMarkReady:
				err = track.MarkReady("audio/a.mp3", ids.Real)
			case domain.EventReplaceAudio:
				err = track.ReplaceAudio("audio/b.mp3", ids.Real)
			case domain.EventMarkFailed:
				err = track.MarkFailed("no match")
			case domain.EventFailPending:
				err = track.FailAcquisition("no match")
			case domain.EventRevertToPending:
				err = track.RevertToPending(now)
			}

			if (err == nil) != (wantErr == nil) {
				t.Errorf("%s from %s: track err=%v, Transition err=%v", event, from, err, wantErr)
			}
		}
	}
}

func trackIn(t *testing.T, status domain.DownloadStatus) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(domain.NewTrackInput{
		UserId: ids.NewUserId(ids.Real()), Title: "Song", Artist: "Artist",
	}, func() time.Time { return time.Unix(1_700_000_000, 0) }, ids.Real)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	switch status {
	case domain.AcquisitionPending:
	case domain.AcquisitionReady:
		if err := track.MarkReady("audio/a.mp3", ids.Real); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
	case domain.AcquisitionFailed:
		if err := track.MarkFailed("first failure"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	}
	return track
}
