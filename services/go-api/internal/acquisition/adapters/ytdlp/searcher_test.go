package ytdlp

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestYtDlpAudioSearcher_Search(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed, skipping integration test")
	}

	// Arrange
	searcher := NewYtDlpAudioSearcher("", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Act: search for a well-known track
	candidates, err := searcher.Search(ctx, "The Weeknd Blinding Lights")

	// Assert
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate, got 0")
	}

	// Verify first candidate has non-empty required fields
	first := candidates[0]
	if first.Title == "" {
		t.Error("first candidate has empty Title")
	}
	if first.URL == "" {
		t.Error("first candidate has empty URL")
	}
	if first.DurationSecs <= 0 {
		t.Errorf("first candidate DurationSecs = %v, want > 0", first.DurationSecs)
	}
}

func TestNewYtDlpAudioSearcher_ReturnsNonNil(t *testing.T) {
	searcher := NewYtDlpAudioSearcher("", "", "")
	if searcher == nil {
		t.Fatal("NewYtDlpAudioSearcher returned nil")
	}
}

func TestNewYtDlpAudioSearcher_StoresConfig(t *testing.T) {
	searcher := NewYtDlpAudioSearcher("/usr/bin/ffmpeg", "/tmp/cookies.txt", "deno")
	if searcher.ffmpegLocation != "/usr/bin/ffmpeg" {
		t.Errorf("ffmpegLocation = %q, want %q", searcher.ffmpegLocation, "/usr/bin/ffmpeg")
	}
	if searcher.cookieFile != "/tmp/cookies.txt" {
		t.Errorf("cookieFile = %q, want %q", searcher.cookieFile, "/tmp/cookies.txt")
	}
	if searcher.jsRuntime != "deno" {
		t.Errorf("jsRuntime = %q, want %q", searcher.jsRuntime, "deno")
	}
}
