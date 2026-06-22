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
		{"search", errors.New("step search: no candidates found"), "no matching audio found"},
		{"select", errors.New("step select: no candidates passed matching gates"), "no matching audio found"},
		{"download", errors.New("step download: download: yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)"), "audio download failed"},
		{"store", errors.New("step store: store audio: disk full"), "audio storage failed"},
		{"cancelled", errors.New("pipeline cancelled: context canceled"), "audio acquisition cancelled"},
		{"unknown step", errors.New("step update_track: persist track update: boom"), "audio acquisition failed"},
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
	err := errors.New("step download: yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")
	if reason := failureReason(err); strings.Contains(reason, "cookies") || strings.Contains(reason, "/home") {
		t.Errorf("failure reason leaked internal details: %q", reason)
	}
}
