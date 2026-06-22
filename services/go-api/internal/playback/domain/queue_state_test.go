package domain

import (
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
			state, err := NewQueueState(testUser(), tt.trackIds, tt.currentIdx, tt.positionMs, false, RepeatOff, "")
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

func TestRehydrateQueueState_RejectsCorruptRow(t *testing.T) {
	// A stored row whose current_idx points past the end must fail to
	// reconstitute rather than ship an invalid snapshot to the client.
	_, err := RehydrateQueueState(testUser(), []string{"a", "b"}, 9, 0, false, RepeatOff, "", time.Now())
	if err == nil {
		t.Fatal("expected out-of-range current_idx to be rejected on rehydrate")
	}
}

func TestRehydrateQueueState_PreservesUpdatedAt(t *testing.T) {
	stored := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	state, err := RehydrateQueueState(testUser(), []string{"a"}, 0, 0, false, RepeatOff, "", stored)
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
