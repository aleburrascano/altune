package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func hangingBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDownload_TimeoutUnderLiveParentIsSourceUnavailable(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = hangingBinary(t)
	s.downloadTimeout = 100 * time.Millisecond

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if !ports.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a source-unavailable error", err)
	}
}

func TestDownload_ParentCancellationIsNotSourceUnavailable(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = hangingBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.Download(ctx, "https://youtube.com/watch?v=1", t.TempDir())

	if err == nil || ports.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a plain error", err)
	}
}
