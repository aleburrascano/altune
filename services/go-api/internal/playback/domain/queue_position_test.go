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
