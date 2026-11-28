package music_test

import (
	"altune/go-api/internal/shared/music"
	"testing"
)

func TestJoinFailureReason(t *testing.T) {
	tests := []struct {
		name   string
		code   music.FailureCode
		detail string
		want   string
	}{
		{"code only", music.FailureNoMatchFound, "", "no_match_found"},
		{"code with detail", music.FailureNoConfidentMatch, "all 1 candidate rejected", "no_confident_match: all 1 candidate rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := music.JoinFailureReason(tt.code, tt.detail); got != tt.want {
				t.Errorf("JoinFailureReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitFailureReason(t *testing.T) {
	tests := []struct {
		name       string
		reason     string
		wantCode   music.FailureCode
		wantDetail string
	}{
		{"code only", "no_match_found", music.FailureNoMatchFound, ""},
		{"code with detail", "no_confident_match: a: b", music.FailureNoConfidentMatch, "a: b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, detail := music.SplitFailureReason(tt.reason)
			if code != tt.wantCode || detail != tt.wantDetail {
				t.Errorf("SplitFailureReason = (%q, %q), want (%q, %q)", code, detail, tt.wantCode, tt.wantDetail)
			}
		})
	}
}

func TestFailureMessage_SplitsCodeFromDetail(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{"code only", "download_failed", "Download failed"},
		{"code plus summary", "download_failed: all 2 candidates rejected", "Download failed"},
		{"unknown code", "mystery: x", "Couldn't get this track"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := music.FailureMessage(&tt.reason); got != tt.want {
				t.Errorf("FailureMessage = %q, want %q", got, tt.want)
			}
		})
	}
}
