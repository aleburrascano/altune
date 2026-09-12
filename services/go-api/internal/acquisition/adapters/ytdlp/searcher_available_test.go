package ytdlp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestYtDlpAudioSearcher_Available_MissingBinary(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp-absent")
	if s.Available() {
		t.Error("Available() = true for a missing yt-dlp binary, want false")
	}
}

func TestYtDlpAudioSearcher_Available_PresentBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = path
	if !s.Available() {
		t.Error("Available() = false for a present yt-dlp binary, want true")
	}
}
