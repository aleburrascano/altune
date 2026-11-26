package ytdlprunner_test

import (
	"altune/go-api/internal/shared/ytdlprunner"
	"testing"
)

func TestOutputShowsSourceUnavailable(t *testing.T) {
	cases := map[string]bool{
		"ERROR: HTTP Error 429: Too Many Requests":        true,
		"Temporary failure in name resolution":            true,
		"read: connection reset by peer":                  true,
		"ERROR: Video unavailable. This video is private": false,
		"ERROR: [youtube] Negp5W0Dcps: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.": true,
		"ERROR: [youtube] Negp5W0Dcps: Sign in to confirm you're not a bot.":                                                                 true,
		"ERROR: [youtube] Negp5W0Dcps: Sign in to confirm your age. This video may be inappropriate for some users.":                         false,
		"": false,
	}
	for output, want := range cases {
		if got := ytdlprunner.OutputShowsSourceUnavailable(output); got != want {
			t.Errorf("OutputShowsSourceUnavailable(%q) = %v, want %v", output, got, want)
		}
	}
}
