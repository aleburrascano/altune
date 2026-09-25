package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With a cookie jar YouTube serves only combined video+audio formats, so a
// bare "bestaudio" matched nothing and every YouTube download failed with
// "Requested format is not available" (#2847).
func formatArgRecorder(t *testing.T, outDir, stdout string) (binary, argsFile string) {
	t.Helper()
	argsFile = filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > \"" + argsFile + "\"\n"
	if outDir != "" {
		script += "truncate -s 1M \"" + filepath.Join(outDir, "out.mp3") + "\"\n"
	}
	if stdout != "" {
		script += "echo '" + stdout + "'\n"
	}
	return withYtDlpScript(t, script), argsFile
}

func formatFlag(t *testing.T, argsFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, a := range args {
		if a == "-f" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no -f flag in yt-dlp args %q", args)
	return ""
}

func TestDownload_FallsBackToACombinedFormatWhenNoAudioOnlyStreamExists(t *testing.T) {
	outDir := t.TempDir()
	s := NewYtDlpAudioSearcher("", "", "")
	var argsFile string
	s.binary, argsFile = formatArgRecorder(t, outDir, "")

	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatalf("Download() = %v, want nil", err)
	}

	got := formatFlag(t, argsFile)
	alternatives := strings.Split(got, "/")
	if alternatives[0] != "bestaudio" {
		t.Errorf("-f %q, want audio-only streams preferred first", got)
	}
	if len(alternatives) < 2 || !strings.HasPrefix(alternatives[1], "best") || alternatives[1] == "bestaudio" {
		t.Errorf("-f %q, want a combined-format fallback after bestaudio", got)
	}
}

func TestCanary_SelectsFormatsTheSameWayDownloadDoes(t *testing.T) {
	outDir := t.TempDir()
	s := NewYtDlpAudioSearcher("", "", "")
	var downloadArgs string
	s.binary, downloadArgs = formatArgRecorder(t, outDir, "")
	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatalf("Download() = %v, want nil", err)
	}

	var canaryArgs string
	s.binary, canaryArgs = formatArgRecorder(t, "", "212")
	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	if got, want := formatFlag(t, canaryArgs), formatFlag(t, downloadArgs); got != want {
		t.Errorf("canary -f %q, want the download's selector %q so a format outage shows up as dark", got, want)
	}
}
