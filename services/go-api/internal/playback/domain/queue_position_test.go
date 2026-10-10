package domain

import "testing"

func TestQueuePositionValidation_EachCauseHasItsOwnCode(t *testing.T) {
	tests := []struct {
		name     string
		input    QueuePositionInput
		wantCode string
	}{
		{
			name:     "negative positionMs",
			input:    QueuePositionInput{CurrentTrackId: "a", PositionMs: -1},
			wantCode: "playback.position_ms_negative",
		},
		{
			name:     "currentIdx past the queue bound",
			input:    QueuePositionInput{CurrentIdx: MaxQueueLength, CurrentTrackId: "a"},
			wantCode: "playback.current_idx_out_of_range",
		},
		{
			name:     "missing currentTrackId",
			input:    QueuePositionInput{CurrentIdx: 0},
			wantCode: "playback.current_track_id_missing",
		},
		{
			name:     "NUL byte in currentTrackId",
			input:    QueuePositionInput{CurrentTrackId: "a\x00b"},
			wantCode: "playback.string_contains_nul",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			in.UserId = testUser()
			_, err := NewQueuePosition(in)

			if got := validationCode(t, err); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestNewQueuePosition_CanonicalizesTheTrackId(t *testing.T) {
	position, err := NewQueuePosition(QueuePositionInput{UserId: testUser(), CurrentTrackId: "3F2504E0-4F89-11D3-9A0C-0305E82C3301"})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}

	if position.CurrentTrackId != "3f2504e0-4f89-11d3-9a0c-0305e82c3301" {
		t.Errorf("CurrentTrackId = %q, want the canonical lowercase form", position.CurrentTrackId)
	}
}

func TestNewQueuePosition_RejectsAnUnparseableTrackId(t *testing.T) {
	_, err := NewQueuePosition(QueuePositionInput{UserId: testUser(), CurrentTrackId: "not-a-uuid"})

	if got := validationCode(t, err); got != "playback.malformed_track_id" {
		t.Errorf("code = %q, want playback.malformed_track_id", got)
	}
}
