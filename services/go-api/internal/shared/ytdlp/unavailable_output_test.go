package ytdlp

import "testing"

func TestOutputShowsSourceUnavailable(t *testing.T) {
	cases := map[string]bool{
		"ERROR: HTTP Error 429: Too Many Requests":        true,
		"Temporary failure in name resolution":            true,
		"read: connection reset by peer":                  true,
		"ERROR: Video unavailable. This video is private": false,
		"": false,
	}
	for output, want := range cases {
		if got := OutputShowsSourceUnavailable(output); got != want {
			t.Errorf("OutputShowsSourceUnavailable(%q) = %v, want %v", output, got, want)
		}
	}
}
