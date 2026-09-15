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
		{"search", &StepError{Step: "search", Err: errors.New("no candidates found")}, "no_match_found"},
		{"select", &StepError{Step: "select", Err: errors.New("no candidates passed matching gates")}, "no_match_found"},
		{"download", &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}, "download_failed"},
		{"store", &StepError{Step: "store", Err: errors.New("store audio: disk full")}, "storage_failed"},
		{"cancelled", errors.New("pipeline cancelled: context canceled"), "acquisition_cancelled"},
		{"unknown step", &StepError{Step: "update_track", Err: errors.New("persist track update: boom")}, "acquisition_failed"},
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

func TestFailureReason_DropsInternalDetails(t *testing.T) {
	err := &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}
	if reason := failureReason(err); strings.Contains(reason, "cookies") || strings.Contains(reason, "/home") {
		t.Errorf("failure reason leaked internal details: %q", reason)
	}
}
