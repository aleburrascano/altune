package domain_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParsePlaylistId(t *testing.T) {
	t.Parallel()
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
			id, err := library.ParsePlaylistId(tt.input)
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
	t.Parallel()
	userId := ids.NewUserId(uuid.New())

	tests := []struct {
		name     string
		plName   string
		wantName string
		wantErr  string
	}{
		{
			name:   "valid name",
			plName: "My Playlist",
		},
		{
			name:    "empty name returns error",
			plName:  "",
			wantErr: "playlist name required",
		},
		{
			name:    "name over 100 chars returns error",
			plName:  strings.Repeat("a", 101),
			wantErr: "playlist name exceeds 100 characters",
		},
		{
			name:   "exactly 100 chars is OK",
			plName: strings.Repeat("a", 100),
		},
		{
			name:    "whitespace-only name returns error",
			plName:  " \t\n ",
			wantErr: "playlist name required",
		},
		{
			name:     "surrounding whitespace is trimmed before the length check",
			plName:   "  " + strings.Repeat("a", 100) + "\t",
			wantName: strings.Repeat("a", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl, err := library.NewPlaylist(userId, tt.plName, testPlaylistCreatedAt)
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
			wantName := tt.plName
			if tt.wantName != "" {
				wantName = tt.wantName
			}
			if pl.Name != wantName {
				t.Errorf("Name = %q, want %q", pl.Name, wantName)
			}
			if !pl.CreatedAt.Equal(testPlaylistCreatedAt) {
				t.Errorf("CreatedAt = %v, want %v", pl.CreatedAt, testPlaylistCreatedAt)
			}
			if !pl.UpdatedAt.Equal(testPlaylistCreatedAt) {
				t.Errorf("UpdatedAt = %v, want %v", pl.UpdatedAt, testPlaylistCreatedAt)
			}
			if len(pl.Tracks) != 0 {
				t.Errorf("expected empty Tracks, got %d", len(pl.Tracks))
			}
		})
	}
}

func TestPlaylist_Rename(t *testing.T) {
	t.Parallel()
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
			wantErr: "playlist name required",
		},
		{
			name:    "name over 100 chars returns error",
			newName: strings.Repeat("x", 101),
			wantErr: "playlist name exceeds 100 characters",
		},
		{
			name:    "whitespace-only name returns error",
			newName: "   \t ",
			wantErr: "playlist name required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl := newTestPlaylist(t)
			renamedAt := testPlaylistCreatedAt.Add(time.Minute)

			err := pl.Rename(tt.newName, renamedAt)
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
			if !pl.UpdatedAt.Equal(renamedAt) {
				t.Errorf("UpdatedAt = %v, want %v after Rename", pl.UpdatedAt, renamedAt)
			}
		})
	}
}

func TestPlaylist_AddTrack(t *testing.T) {
	t.Parallel()
	t.Run("adds track at correct position", func(t *testing.T) {
		pl := newTestPlaylist(t)
		addedAAt := testPlaylistCreatedAt.Add(time.Minute)
		addedBAt := addedAAt.Add(time.Minute)

		trackA := library.NewTrackId()
		trackB := library.NewTrackId()

		if err := pl.AddTrack(trackA, addedAAt); err != nil {
			t.Fatalf("AddTrack(A) unexpected error: %v", err)
		}
		if !pl.UpdatedAt.Equal(addedAAt) {
			t.Errorf("UpdatedAt = %v, want %v after AddTrack(A)", pl.UpdatedAt, addedAAt)
		}
		if err := pl.AddTrack(trackB, addedBAt); err != nil {
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
		if !pl.UpdatedAt.Equal(addedBAt) {
			t.Errorf("UpdatedAt = %v, want %v after AddTrack(B)", pl.UpdatedAt, addedBAt)
		}
	})

	t.Run("takes the last slot then refuses with ErrPlaylistFull", func(t *testing.T) {
		pl := newTestPlaylist(t)
		for range library.MaxPlaylistTracks - 1 {
			if err := pl.AddTrack(library.NewTrackId(), testPlaylistCreatedAt); err != nil {
				t.Fatalf("AddTrack below the cap: %v", err)
			}
		}
		if err := pl.AddTrack(library.NewTrackId(), testPlaylistCreatedAt); err != nil {
			t.Fatalf("AddTrack into the last slot: %v", err)
		}

		err := pl.AddTrack(library.NewTrackId(), testPlaylistCreatedAt)

		if !errors.Is(err, library.ErrPlaylistFull) {
			t.Fatalf("AddTrack past the cap: err = %v, want ErrPlaylistFull", err)
		}
		if len(pl.Tracks) != library.MaxPlaylistTracks {
			t.Errorf("tracks = %d, want %d", len(pl.Tracks), library.MaxPlaylistTracks)
		}
	})

	t.Run("duplicate track returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()

		if err := pl.AddTrack(trackA, testPlaylistCreatedAt); err != nil {
			t.Fatalf("first AddTrack unexpected error: %v", err)
		}

		err := pl.AddTrack(trackA, testPlaylistCreatedAt)
		if err == nil {
			t.Fatal("expected error for duplicate track, got nil")
		}
		if err.Error() != "track already in playlist" {
			t.Errorf("error = %q, want %q", err.Error(), "track already in playlist")
		}
	})
}

func TestPlaylist_RemoveTrack(t *testing.T) {
	t.Parallel()
	t.Run("removes and reorders positions", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()
		trackB := library.NewTrackId()
		trackC := library.NewTrackId()

		for _, id := range []library.TrackId{trackA, trackB, trackC} {
			if err := pl.AddTrack(id, testPlaylistCreatedAt); err != nil {
				t.Fatalf("AddTrack setup failed: %v", err)
			}
		}

		removedAt := testPlaylistCreatedAt.Add(time.Hour)

		removed := pl.RemoveTrack(trackB, removedAt)
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
		if !pl.UpdatedAt.Equal(removedAt) {
			t.Errorf("UpdatedAt = %v, want %v after RemoveTrack", pl.UpdatedAt, removedAt)
		}
	})

	t.Run("absent track returns false", func(t *testing.T) {
		pl := newTestPlaylist(t)
		absent := library.NewTrackId()

		removed := pl.RemoveTrack(absent, testPlaylistCreatedAt)
		if removed {
			t.Error("expected RemoveTrack to return false for absent track")
		}
	})
}

func TestPlaylist_Reorder(t *testing.T) {
	t.Parallel()
	t.Run("valid reorder", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()
		trackB := library.NewTrackId()
		trackC := library.NewTrackId()

		for _, id := range []library.TrackId{trackA, trackB, trackC} {
			if err := pl.AddTrack(id, testPlaylistCreatedAt); err != nil {
				t.Fatalf("AddTrack setup failed: %v", err)
			}
		}

		reorderedAt := testPlaylistCreatedAt.Add(time.Hour)

		err := pl.Reorder([]library.TrackId{trackC, trackB, trackA}, reorderedAt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pl.Tracks) != 3 {
			t.Fatalf("expected 3 tracks, got %d", len(pl.Tracks))
		}
		wantOrder := []library.TrackId{trackC, trackB, trackA}
		for i, want := range wantOrder {
			if pl.Tracks[i].TrackId != want {
				t.Errorf("Tracks[%d].TrackId = %v, want %v", i, pl.Tracks[i].TrackId, want)
			}
			if pl.Tracks[i].Position != i {
				t.Errorf("Tracks[%d].Position = %d, want %d", i, pl.Tracks[i].Position, i)
			}
		}
		if !pl.UpdatedAt.Equal(reorderedAt) {
			t.Errorf("UpdatedAt = %v, want %v after Reorder", pl.UpdatedAt, reorderedAt)
		}
	})

	t.Run("length mismatch returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()
		if err := pl.AddTrack(trackA, testPlaylistCreatedAt); err != nil {
			t.Fatalf("AddTrack setup failed: %v", err)
		}

		err := pl.Reorder([]library.TrackId{trackA, library.NewTrackId()}, testPlaylistCreatedAt)
		if err == nil {
			t.Fatal("expected error for length mismatch, got nil")
		}
		if err.Error() != "track list length mismatch" {
			t.Errorf("error = %q, want %q", err.Error(), "track list length mismatch")
		}
	})

	t.Run("unknown track returns error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()
		if err := pl.AddTrack(trackA, testPlaylistCreatedAt); err != nil {
			t.Fatalf("AddTrack setup failed: %v", err)
		}

		err := pl.Reorder([]library.TrackId{library.NewTrackId()}, testPlaylistCreatedAt)
		if err == nil {
			t.Fatal("expected error for unknown track, got nil")
		}
		if err.Error() != "unknown track in reorder list" {
			t.Errorf("error = %q, want %q", err.Error(), "unknown track in reorder list")
		}
	})

	t.Run("duplicate track returns validation error", func(t *testing.T) {
		pl := newTestPlaylist(t)
		trackA := library.NewTrackId()
		trackB := library.NewTrackId()
		for _, id := range []library.TrackId{trackA, trackB} {
			if err := pl.AddTrack(id, testPlaylistCreatedAt); err != nil {
				t.Fatalf("AddTrack setup failed: %v", err)
			}
		}

		err := pl.Reorder([]library.TrackId{trackA, trackA}, testPlaylistCreatedAt)
		if err == nil {
			t.Fatal("expected error for duplicate track, got nil")
		}
		dbtest.AssertValidationError(t, err)
		if len(pl.Tracks) != 2 {
			t.Fatalf("expected tracks unchanged (2), got %d", len(pl.Tracks))
		}
	})
}

func TestPlaylist_StampsInjectedTimeAsUTC(t *testing.T) {
	t.Parallel()
	local := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.FixedZone("UTC+2", 2*60*60))

	pl, err := library.NewPlaylist(ids.NewUserId(uuid.New()), "Zoned", local)
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}
	if pl.CreatedAt.Location() != time.UTC || !pl.CreatedAt.Equal(local) {
		t.Errorf("CreatedAt = %v, want %v in UTC", pl.CreatedAt, local)
	}
	if err := pl.Rename("Zoned again", local.Add(time.Minute)); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if pl.UpdatedAt.Location() != time.UTC || !pl.UpdatedAt.Equal(local.Add(time.Minute)) {
		t.Errorf("UpdatedAt = %v, want %v in UTC", pl.UpdatedAt, local.Add(time.Minute))
	}
}

var testPlaylistCreatedAt = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

func newTestPlaylist(t *testing.T) *library.Playlist {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	pl, err := library.NewPlaylist(userId, "Test Playlist", testPlaylistCreatedAt)
	if err != nil {
		t.Fatalf("newTestPlaylist: unexpected error: %v", err)
	}
	return pl
}

func TestCatalogDomainErrorCodes(t *testing.T) {
	if got := library.ErrTrackAlreadyInPlaylist.Code().String(); got != "catalog.track_already_in_playlist" {
		t.Errorf("ErrTrackAlreadyInPlaylist code: got %q", got)
	}
	invalid := library.NewValidationError("x")
	if got := invalid.Code().String(); got != "catalog.validation_error" {
		t.Errorf("ValidationError code: got %q", got)
	}
	if invalid.Error() != "x" {
		t.Errorf("ValidationError detail: got %q, want %q", invalid.Error(), "x")
	}
}
