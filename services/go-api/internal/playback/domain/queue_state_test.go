package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"altune/go-api/internal/shared"
)

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestNewQueueState_Validation(t *testing.T) {
	tests := []struct {
		name       string
		trackIds   []string
		currentIdx int
		positionMs int64
		wantErr    bool
		wantIdx    int
	}{
		{name: "valid in-range", trackIds: []string{"a", "b", "c"}, currentIdx: 1, positionMs: 5000, wantIdx: 1},
		{name: "empty queue normalizes idx to 0", trackIds: []string{}, currentIdx: -1, positionMs: 0, wantIdx: 0},
		{name: "idx past end rejected", trackIds: []string{"a", "b"}, currentIdx: 2, positionMs: 0, wantErr: true},
		{name: "negative idx rejected", trackIds: []string{"a"}, currentIdx: -1, positionMs: 0, wantErr: true},
		{name: "negative position rejected", trackIds: []string{"a"}, currentIdx: 0, positionMs: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := NewQueueState(QueueStateInput{
				UserId:     testUser(),
				TrackIds:   tt.trackIds,
				CurrentIdx: tt.currentIdx,
				PositionMs: tt.positionMs,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.CurrentIdx != tt.wantIdx {
				t.Errorf("CurrentIdx = %d, want %d", state.CurrentIdx, tt.wantIdx)
			}
		})
	}
}

func TestRehydrateQueueState_RejectsStoredRowWithCurrentIdxPastEnd(t *testing.T) {
	_, err := RehydrateQueueState(QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 9,
	}, time.Now())
	if err == nil {
		t.Fatal("expected out-of-range current_idx to be rejected on rehydrate")
	}
}

func TestRehydrateQueueState_PreservesUpdatedAt(t *testing.T) {
	stored := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	state, err := RehydrateQueueState(QueueStateInput{
		UserId:   testUser(),
		TrackIds: []string{"a"},
	}, stored)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.UpdatedAt.Equal(stored) {
		t.Errorf("UpdatedAt = %v, want %v (stored value, not now)", state.UpdatedAt, stored)
	}
}

func TestEmptyQueueState_IsValidAndEmpty(t *testing.T) {
	state := EmptyQueueState(testUser())
	if len(state.TrackIds) != 0 {
		t.Errorf("TrackIds = %v, want empty", state.TrackIds)
	}
	if state.CurrentIdx != 0 {
		t.Errorf("CurrentIdx = %d, want 0", state.CurrentIdx)
	}
	if state.RepeatMode != RepeatOff {
		t.Errorf("RepeatMode = %v, want RepeatOff", state.RepeatMode)
	}
}

func repeatIds(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "a"
	}
	return ids
}

func TestNewQueueState_BoundsQueueLength(t *testing.T) {
	tests := []struct {
		name         string
		trackIds     []string
		naturalOrder []string
		wantErr      bool
	}{
		{name: "trackIds at limit accepted", trackIds: repeatIds(MaxQueueLength)},
		{name: "trackIds over limit rejected", trackIds: repeatIds(MaxQueueLength + 1), wantErr: true},
		{name: "naturalOrder at limit accepted", trackIds: repeatIds(1), naturalOrder: repeatIds(MaxQueueLength)},
		{name: "naturalOrder over limit rejected", trackIds: repeatIds(1), naturalOrder: repeatIds(MaxQueueLength + 1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQueueState(QueueStateInput{
				UserId:       testUser(),
				TrackIds:     tt.trackIds,
				NaturalOrder: tt.naturalOrder,
			})
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected over-limit queue to be rejected")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ValidationError", err)
			}
		})
	}
}

func TestNewQueueState_RejectsNulBytes(t *testing.T) {
	tests := []struct {
		name  string
		input QueueStateInput
	}{
		{
			name:  "nul in trackIds",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{"a\x00b"}, CurrentIdx: 0},
		},
		{
			name:  "nul in naturalOrder",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, NaturalOrder: []string{"x\x00y"}},
		},
		{
			name:  "nul in sourceId",
			input: QueueStateInput{UserId: testUser(), SourceId: "playlist:pid:na\x00me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQueueState(tt.input)
			if err == nil {
				t.Fatal("expected NUL byte in string element to be rejected")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ValidationError", err)
			}
		})
	}
}

func TestRepeatMode_RoundTrip(t *testing.T) {
	for _, rm := range []RepeatMode{RepeatOff, RepeatAll, RepeatOne} {
		parsed, err := ParseRepeatMode(rm.String())
		if err != nil {
			t.Fatalf("ParseRepeatMode(%q): %v", rm.String(), err)
		}
		if parsed != rm {
			t.Errorf("round-trip %v -> %q -> %v", rm, rm.String(), parsed)
		}
	}
}

func TestPlaybackValidationErrorCode(t *testing.T) {
	if got := (&ValidationError{Message: "x"}).ErrorCode(); got != "playback.validation_error" {
		t.Errorf("code: got %q, want %q", got, "playback.validation_error")
	}
}
