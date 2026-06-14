package domain

import (
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestParsePlaylistId(t *testing.T) {
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
			id, err := ParsePlaylistId(tt.input)
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

func TestNewPlaylist(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	tests := []struct {
		name    string
		plName  string
		wantErr string
	}{
		{
			name:   "valid name",
			plName: "My Playlist",
		},
		{
			name:    "empty name returns error",
			plName:  "",
			wantErr: "playlist name cannot be empty",
		},
		{
			name:    "name over 100 chars returns error",
			plName:  strings.Repeat("a", 101),
			wantErr: "playlist name cannot exceed 100 characters",
		},
		{
			name:   "exactly 100 chars is OK",
			plName: strings.Repeat("a", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl, err := NewPlaylist(userId, tt.plName)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				if pl != nil {
					t.Error("expected nil playlist on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pl.ID.IsZero() {
				t.Error("expected non-zero ID")
			}
			if pl.UserId != userId {
				t.Errorf("UserId = %v, want %v", pl.UserId, userId)
			}
			if pl.Name != tt.plName {
				t.Errorf("Name = %q, want %q", pl.Name, tt.plName)
			}
			if pl.CreatedAt.IsZero() {
				t.Error("expected non-zero CreatedAt")
			}
			if pl.UpdatedAt.IsZero() {
				t.Error("expected non-zero UpdatedAt")
			}
			if len(pl.Tracks) != 0 {
				t.Errorf("expected empty Tracks, got %d", len(pl.Tracks))
			}
		})
	}
}

func TestPlaylist_Rename(t *testing.T) {
	tests := []struct {
		name    string
		newName string
		wantErr string
	}{
		{
			name:    "valid new name",
			newName: "Renamed Playlist",
		},
		{
			name:    "empty name returns error",
			newName: "",
			wantErr: "playlist name cannot be empty",
		},
		{
			name:    "name over 100 chars returns error",
			newName: strings.Repeat("x", 101),
			wantErr: "playlist name cannot exceed 100 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl := newTestPlaylist(t)
			beforeUpdate := pl.UpdatedAt
			// Ensure time difference is detectable
			time.Sleep(time.Millisecond)

			err := pl.Rename(tt.newName)
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
			if pl.Name != tt.newName {
				t.Errorf("Name = %q, want %q", pl.Name, tt.newName)
			}
			if !pl.UpdatedAt.After(beforeUpdate) {
				t.Error("expected UpdatedAt to be updated after Rename")
			}
		})
	}
}

func TestPlaylist_AddTrack(t *testing.T) {
	t.Run("adds track at correct position", func(t *testing.T) {
		pl := newTestPlaylist(t)
		beforeUpdate := pl.UpdatedAt
		time.Sleep(time.Millisecond)

		trackA := NewTrackId()
		trackB := NewTrackId()

		if err := pl.AddTrack(trackA); err != nil {
			t.Fatalf("AddTrack(A) unexpected error: %v", err)
		}
		if err := pl.AddTrack(trackB); err != nil {
			t.Fatalf("AddTrack(B) unexpected error: %v", err)
		}

		if len(pl.Tracks) != 2 {
			t.Fatalf("expected 2 tracks, got %d", len(pl.Tracks))
		}
		if pl.Tracks[0].TrackId != trackA || pl.Tracks[0].Position != 0 {
			t.Errorf("Tracks[0] = {%v, %d}, want {%v, 0}", pl.Tracks[0].TrackId, pl.Tracks[0].Position, trackA)
		}
		if pl.Tracks[1].TrackId != trackB || pl.Tracks[1].Position != 1 {
			t.Errorf("Tracks[1] = {%v, %d}, want {%v, 1}", pl.Tracks[1].TrackId, pl.Tracks[1].Position, trackB)
		}
		if !pl.UpdatedAt.After(beforeUpdate) {
			t.Error("expected UpdatedAt to be updated after AddTrack")
		}
	})

	t.Run("duplicate track returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := NewTrackId()

		if err := pl.AddTrack(trackA); err != nil {
			t.Fatalf("first AddTrack unexpected error: %v", err)
		}

		err := pl.AddTrack(trackA)
		if err == nil {
			t.Fatal("expected error for duplicate track, got nil")
		}
		if err.Error() != "track already in playlist" {
			t.Errorf("error = %q, want %q", err.Error(), "track already in playlist")
		}
	})
}

func TestPlaylist_RemoveTrack(t *testing.T) {
	t.Run("removes and reorders positions", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := NewTrackId()
		trackB := NewTrackId()
		trackC := NewTrackId()

		for _, id := range []TrackId{trackA, trackB, trackC} {
			if err := pl.AddTrack(id); err != nil {
				t.Fatalf("AddTrack setup failed: %v", err)
			}
		}

		beforeUpdate := pl.UpdatedAt
		time.Sleep(time.Millisecond)

		removed := pl.RemoveTrack(trackB)
		if !removed {
			t.Fatal("expected RemoveTrack to return true")
		}

		if len(pl.Tracks) != 2 {
			t.Fatalf("expected 2 tracks after removal, got %d", len(pl.Tracks))
		}
		if pl.Tracks[0].TrackId != trackA || pl.Tracks[0].Position != 0 {
			t.Errorf("Tracks[0] = {%v, %d}, want {%v, 0}", pl.Tracks[0].TrackId, pl.Tracks[0].Position, trackA)
		}
		if pl.Tracks[1].TrackId != trackC || pl.Tracks[1].Position != 1 {
			t.Errorf("Tracks[1] = {%v, %d}, want {%v, 1}", pl.Tracks[1].TrackId, pl.Tracks[1].Position, trackC)
		}
		if !pl.UpdatedAt.After(beforeUpdate) {
			t.Error("expected UpdatedAt to be updated after RemoveTrack")
		}
	})

	t.Run("absent track returns false", func(t *testing.T) {
		pl := newTestPlaylist(t)
		absent := NewTrackId()

		removed := pl.RemoveTrack(absent)
		if removed {
			t.Error("expected RemoveTrack to return false for absent track")
		}
	})
}

func TestPlaylist_Reorder(t *testing.T) {
	t.Run("valid reorder", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := NewTrackId()
		trackB := NewTrackId()
		trackC := NewTrackId()

		for _, id := range []TrackId{trackA, trackB, trackC} {
			if err := pl.AddTrack(id); err != nil {
				t.Fatalf("AddTrack setup failed: %v", err)
			}
		}

		beforeUpdate := pl.UpdatedAt
		time.Sleep(time.Millisecond)

		// Reverse order: C, B, A
		err := pl.Reorder([]TrackId{trackC, trackB, trackA})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pl.Tracks) != 3 {
			t.Fatalf("expected 3 tracks, got %d", len(pl.Tracks))
		}
		wantOrder := []TrackId{trackC, trackB, trackA}
		for i, want := range wantOrder {
			if pl.Tracks[i].TrackId != want {
				t.Errorf("Tracks[%d].TrackId = %v, want %v", i, pl.Tracks[i].TrackId, want)
			}
			if pl.Tracks[i].Position != i {
				t.Errorf("Tracks[%d].Position = %d, want %d", i, pl.Tracks[i].Position, i)
			}
		}
		if !pl.UpdatedAt.After(beforeUpdate) {
			t.Error("expected UpdatedAt to be updated after Reorder")
		}
	})

	t.Run("length mismatch returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := NewTrackId()
		if err := pl.AddTrack(trackA); err != nil {
			t.Fatalf("AddTrack setup failed: %v", err)
		}

		err := pl.Reorder([]TrackId{trackA, NewTrackId()})
		if err == nil {
			t.Fatal("expected error for length mismatch, got nil")
		}
		if err.Error() != "track list length mismatch" {
			t.Errorf("error = %q, want %q", err.Error(), "track list length mismatch")
		}
	})

	t.Run("unknown track returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := NewTrackId()
		if err := pl.AddTrack(trackA); err != nil {
			t.Fatalf("AddTrack setup failed: %v", err)
		}

		err := pl.Reorder([]TrackId{NewTrackId()})
		if err == nil {
			t.Fatal("expected error for unknown track, got nil")
		}
		if err.Error() != "unknown track in reorder list" {
			t.Errorf("error = %q, want %q", err.Error(), "unknown track in reorder list")
		}
	})
}

// --- helpers ---

func newTestPlaylist(t *testing.T) *Playlist {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	pl, err := NewPlaylist(userId, "Test Playlist")
	if err != nil {
		t.Fatalf("newTestPlaylist: unexpected error: %v", err)
	}
	return pl
}
