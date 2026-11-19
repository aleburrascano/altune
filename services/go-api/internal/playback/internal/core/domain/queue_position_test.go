package domain_test

import (
	"altune/go-api/internal/playback"
	"testing"
)

func TestQueuePositionValidation_EachCauseHasItsOwnCode(t *testing.T) {
	tests := []struct {
		name     string
		input    playback.SaveQueuePositionInput
		wantCode string
	}{
		{
			name:     "negative positionMs",
			input:    playback.SaveQueuePositionInput{CurrentTrackId: "a", PositionMs: -1},
			wantCode: "playback.position_ms_negative",
		},
		{
			name:     "currentIdx past the queue bound",
			input:    playback.SaveQueuePositionInput{CurrentIdx: maxQueueLength, CurrentTrackId: "a"},
			wantCode: "playback.current_idx_out_of_range",
		},
		{
			name:     "missing currentTrackId",
			input:    playback.SaveQueuePositionInput{CurrentIdx: 0},
			wantCode: "playback.current_track_id_missing",
		},
		{
			name:     "NUL byte in currentTrackId",
			input:    playback.SaveQueuePositionInput{CurrentTrackId: "a\x00b"},
			wantCode: "playback.string_contains_nul",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := savePosition(tt.input)

			if got := validationCode(t, err); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestQueuePosition_LastIndexInsideTheBoundIsAccepted(t *testing.T) {
	position, err := savePosition(playback.SaveQueuePositionInput{CurrentIdx: maxQueueLength - 1, CurrentTrackId: canonicalTrackA})
	if err != nil {
		t.Fatalf("currentIdx %d is inside the queue bound: %v", maxQueueLength-1, err)
	}
	if position.CurrentIdx != maxQueueLength-1 {
		t.Errorf("CurrentIdx = %d, want %d", position.CurrentIdx, maxQueueLength-1)
	}
}

func TestNewQueuePosition_CanonicalizesTheTrackId(t *testing.T) {
	position, err := savePosition(playback.SaveQueuePositionInput{CurrentTrackId: "3F2504E0-4F89-11D3-9A0C-0305E82C3301"})
	if err != nil {
		t.Fatalf("SavePosition: %v", err)
	}

	if position.CurrentTrackId != canonicalTrackA {
		t.Errorf("CurrentTrackId = %q, want the canonical lowercase form", position.CurrentTrackId)
	}
}

func TestNewQueuePosition_RejectsAnUnparseableTrackId(t *testing.T) {
	_, err := savePosition(playback.SaveQueuePositionInput{CurrentTrackId: "not-a-uuid"})

	if got := validationCode(t, err); got != "playback.malformed_track_id" {
		t.Errorf("code = %q, want playback.malformed_track_id", got)
	}
}
