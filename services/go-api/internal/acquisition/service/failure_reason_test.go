package service

import (
	"errors"
	"strings"
	"testing"
)

func TestFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"search", &StepError{Step: "search", Err: errors.New("no candidates found")}, "no matching audio found"},
		{"select", &StepError{Step: "select", Err: errors.New("no candidates passed matching gates")}, "no matching audio found"},
		{"download", &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}, "audio download failed"},
		{"store", &StepError{Step: "store", Err: errors.New("store audio: disk full")}, "audio storage failed"},
		{"cancelled", errors.New("pipeline cancelled: context canceled"), "audio acquisition cancelled"},
		{"unknown step", &StepError{Step: "update_track", Err: errors.New("persist track update: boom")}, "audio acquisition failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failureReason(tt.err)
			if got != tt.want {
				t.Errorf("failureReason(%q) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// The whole point of failureReason is to keep internals out of the stored/wire
// reason: the download case must not leak yt-dlp stderr (file paths, etc.).
func TestFailureReason_DropsInternalDetails(t *testing.T) {
	err := &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}
	if reason := failureReason(err); strings.Contains(reason, "cookies") || strings.Contains(reason, "/home") {
		t.Errorf("failure reason leaked internal details: %q", reason)
	}
}
