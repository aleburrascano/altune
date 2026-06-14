package domain

import (
	"testing"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestParseTrackId(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantStr string
	}{
		{
			name:    "valid UUID",
			input:   validUUID,
			wantErr: false,
			wantStr: validUUID,
		},
		{
			name:    "invalid UUID",
			input:   "not-a-uuid",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseTrackId(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := id.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestTrackIdFromUUID(t *testing.T) {
	raw := uuid.New()
	id := TrackIdFromUUID(raw)

	if id.UUID() != raw {
		t.Errorf("UUID() = %v, want %v", id.UUID(), raw)
	}
	if id.String() != raw.String() {
		t.Errorf("String() = %q, want %q", id.String(), raw.String())
	}
}

func TestTrackId_IsZero(t *testing.T) {
	tests := []struct {
		name string
		id   TrackId
		want bool
	}{
		{
			name: "nil UUID is zero",
			id:   TrackIdFromUUID(uuid.Nil),
			want: true,
		},
		{
			name: "non-nil UUID is not zero",
			id:   TrackIdFromUUID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")),
			want: false,
		},
		{
			name: "default value is zero",
			id:   TrackId{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAcquisitionStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    AcquisitionStatus
		wantErr bool
	}{
		{name: "pending", input: "pending", want: AcquisitionPending},
		{name: "ready", input: "ready", want: AcquisitionReady},
		{name: "failed", input: "failed", want: AcquisitionFailed},
		{name: "invalid value", input: "downloading", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAcquisitionStatus(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseAcquisitionStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAcquisitionStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status AcquisitionStatus
		want   string
	}{
		{name: "pending", status: AcquisitionPending, want: "pending"},
		{name: "ready", status: AcquisitionReady, want: "ready"},
		{name: "failed", status: AcquisitionFailed, want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	tests := []struct {
		name    string
		title   string
		artist  string
		album   string
		wantErr string
	}{
		{
			name:   "valid with all fields",
			title:  "Song Title",
			artist: "Artist Name",
			album:  "Album Name",
		},
		{
			name:   "valid with empty album",
			title:  "Song Title",
			artist: "Artist Name",
			album:  "",
		},
		{
			name:    "empty title returns error",
			title:   "",
			artist:  "Artist Name",
			album:   "Album Name",
			wantErr: "track title is required",
		},
		{
			name:    "empty artist returns error",
			title:   "Song Title",
			artist:  "",
			album:   "Album Name",
			wantErr: "track artist is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track, err := NewTrack(userId, tt.title, tt.artist, tt.album)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				if track != nil {
					t.Error("expected nil track on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.ID.IsZero() {
				t.Error("expected non-zero ID")
			}
			if track.UserId != userId {
				t.Errorf("UserId = %v, want %v", track.UserId, userId)
			}
			if track.Title != tt.title {
				t.Errorf("Title = %q, want %q", track.Title, tt.title)
			}
			if track.Artist != tt.artist {
				t.Errorf("Artist = %q, want %q", track.Artist, tt.artist)
			}
			if track.Album != tt.album {
				t.Errorf("Album = %q, want %q", track.Album, tt.album)
			}
			if track.AcquisitionStatus != AcquisitionPending {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.AcquisitionStatus)
			}
			if track.DedupKey == "" {
				t.Error("expected non-empty DedupKey")
			}
			if track.DedupKey != ComputeDedupKey(tt.title, tt.artist, tt.album) {
				t.Errorf("DedupKey = %q, want %q", track.DedupKey, ComputeDedupKey(tt.title, tt.artist, tt.album))
			}
			if track.AddedAt.IsZero() {
				t.Error("expected non-zero AddedAt")
			}
		})
	}
}

func TestTrack_MarkReady(t *testing.T) {
	tests := []struct {
		name     string
		audioRef string
		wantErr  string
	}{
		{
			name:     "valid audioRef sets status and AudioRef",
			audioRef: "s3://bucket/track.opus",
		},
		{
			name:     "empty audioRef returns error",
			audioRef: "",
			wantErr:  "audio_ref is required to mark track as ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := newTestTrack(t)
			// Pre-set a failure reason to verify it gets cleared
			reason := "old failure"
			track.FailureReason = &reason

			err := track.MarkReady(tt.audioRef)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.AcquisitionStatus != AcquisitionReady {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionReady", track.AcquisitionStatus)
			}
			if track.AudioRef == nil || *track.AudioRef != tt.audioRef {
				t.Errorf("AudioRef = %v, want %q", track.AudioRef, tt.audioRef)
			}
			if track.FailureReason != nil {
				t.Errorf("FailureReason should be nil after MarkReady, got %q", *track.FailureReason)
			}
		})
	}
}

func TestTrack_MarkFailed(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		wantErr string
	}{
		{
			name:   "valid reason sets status and FailureReason",
			reason: "download timeout",
		},
		{
			name:    "empty reason returns error",
			reason:  "",
			wantErr: "failure_reason is required to mark track as failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := newTestTrack(t)
			// Pre-set an audio ref to verify it gets cleared
			ref := "s3://bucket/track.opus"
			track.AudioRef = &ref

			err := track.MarkFailed(tt.reason)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.AcquisitionStatus != AcquisitionFailed {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionFailed", track.AcquisitionStatus)
			}
			if track.FailureReason == nil || *track.FailureReason != tt.reason {
				t.Errorf("FailureReason = %v, want %q", track.FailureReason, tt.reason)
			}
			if track.AudioRef != nil {
				t.Errorf("AudioRef should be nil after MarkFailed, got %q", *track.AudioRef)
			}
		})
	}
}

func TestTrack_RevertToPending(t *testing.T) {
	track := newTestTrack(t)

	// Put track into ready state first
	if err := track.MarkReady("s3://bucket/track.opus"); err != nil {
		t.Fatalf("setup: MarkReady failed: %v", err)
	}

	track.RevertToPending()

	if track.AcquisitionStatus != AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.AcquisitionStatus)
	}
	if track.AudioRef != nil {
		t.Errorf("AudioRef should be nil after RevertToPending, got %q", *track.AudioRef)
	}
	if track.FailureReason != nil {
		t.Errorf("FailureReason should be nil after RevertToPending, got %q", *track.FailureReason)
	}
}

func TestTrack_IsStreamable(t *testing.T) {
	audioRef := "s3://bucket/track.opus"

	tests := []struct {
		name   string
		setup  func(*Track)
		want   bool
	}{
		{
			name: "ready with audioRef is streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionReady
				tr.AudioRef = &audioRef
			},
			want: true,
		},
		{
			name: "ready without audioRef is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionReady
				tr.AudioRef = nil
			},
			want: false,
		},
		{
			name: "pending is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionPending
			},
			want: false,
		},
		{
			name: "failed is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionFailed
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := newTestTrack(t)
			tt.setup(track)
			if got := track.IsStreamable(); got != tt.want {
				t.Errorf("IsStreamable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- helpers ---

func newTestTrack(t *testing.T) *Track {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := NewTrack(userId, "Test Title", "Test Artist", "Test Album")
	if err != nil {
		t.Fatalf("newTestTrack: unexpected error: %v", err)
	}
	return track
}
