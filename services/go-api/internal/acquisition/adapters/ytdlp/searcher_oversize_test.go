package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYtDlpAudioSearcher_Download_RejectsAnOversizeOutput(t *testing.T) {
	outDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "big-yt-dlp")
	script := "#!/bin/sh\ntruncate -s 210M \"" + filepath.Join(outDir, "big.mp3") + "\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Download error = %v, want a too-large rejection", err)
	}
}
