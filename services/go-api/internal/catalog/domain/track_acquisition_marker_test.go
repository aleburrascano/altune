package domain

import (
	"altune/go-api/internal/shared"
	"testing"

	"github.com/google/uuid"
)

func markerUserId(t *testing.T) shared.UserId {
	t.Helper()
	return shared.NewUserId(uuid.New())
}

func TestNewTrack_SetsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if track.AcquisitionStatus != AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.AcquisitionStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("AcquisitionStartedAt = nil, want the in-flight marker set at creation")
	}
	if !track.AcquisitionStartedAt.Equal(track.AddedAt) {
		t.Errorf("AcquisitionStartedAt = %v, want it to equal AddedAt %v", track.AcquisitionStartedAt, track.AddedAt)
	}
}

func TestTrack_MarkReady_ClearsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkReady("s3://bucket/audio.opus"); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if track.AcquisitionStartedAt != nil {
		t.Errorf("AcquisitionStartedAt = %v, want nil after MarkReady", track.AcquisitionStartedAt)
	}
}

func TestTrack_MarkFailed_ClearsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed(string(FailureAcquisitionInterrupted)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if track.AcquisitionStartedAt != nil {
		t.Errorf("AcquisitionStartedAt = %v, want nil after MarkFailed", track.AcquisitionStartedAt)
	}
}

func TestTrack_RevertToPending_RefreshesInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if err := track.RevertToPending(); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}

	if track.AcquisitionStatus != AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.AcquisitionStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("AcquisitionStartedAt = nil, want a fresh marker after RevertToPending")
	}
}

func TestFailureMessage_AcquisitionInterrupted(t *testing.T) {
	t.Parallel()
	reason := string(FailureAcquisitionInterrupted)
	if got := FailureMessage(&reason); got != "Acquisition was interrupted" {
		t.Errorf("FailureMessage = %q, want %q", got, "Acquisition was interrupted")
	}
}

// Both codes are persisted in failure_reason and published in the
// track_acquisition_failed payload, so renaming a value strands every stored
// row and shipped client that already carries the old one.
func TestAcquisitionFailureCodes_KeepTheirStoredValues(t *testing.T) {
	t.Parallel()
	stored := []struct {
		code FailureCode
		want string
	}{
		{FailureAcquisitionInterrupted, "acquisition_interrupted"},
		{FailureAcquisitionRefused, "acquisition_refused"},
	}
	for _, s := range stored {
		if string(s.code) != s.want {
			t.Errorf("stored failure code = %q, want %q", s.code, s.want)
		}
	}
}
