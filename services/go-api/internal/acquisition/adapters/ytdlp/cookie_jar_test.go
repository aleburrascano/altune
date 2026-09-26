package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const liveJarContents = "# live cookie jar"

// jarWritingYtDlpAt writes an executable that behaves like yt-dlp on exit: it
// saves the cookie jar back to whatever file follows --cookies, failing the run
// when it cannot. It then prints one search entry and produces one mp3 in
// outDir, so the same stub serves Search and Download.
func jarWritingYtDlpAt(t *testing.T, path, outDir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--cookies\" ]; then echo '# saved by yt-dlp' > \"$arg\" || exit 1; fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"echo '{\"title\":\"Song\",\"webpage_url\":\"https://www.youtube.com/watch?v=aaaaaaaaaaa\"}'\n" +
		"head -c 20480 /dev/zero > \"" + filepath.Join(outDir, "stub.mp3") + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// readOnlyLiveJar mirrors staging, which mounts the shared jar read-only.
func readOnlyLiveJar(t *testing.T) string {
	t.Helper()
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte(liveJarContents), 0o444); err != nil {
		t.Fatal(err)
	}
	return jar
}

func assertLiveJarUntouched(t *testing.T, jar string) {
	t.Helper()
	got, err := os.ReadFile(jar)
	if err != nil || string(got) != liveJarContents {
		t.Fatalf("live cookie jar = %q, err %v, want it untouched", got, err)
	}
}

func TestYtDlpAudioSearcher_Search_WorksAgainstAReadOnlyCookieJar(t *testing.T) {
	binDir := t.TempDir()
	jarWritingYtDlpAt(t, filepath.Join(binDir, "yt-dlp"), t.TempDir())
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := readOnlyLiveJar(t)

	s := NewYtDlpAudioSearcher("", jar, "")
	got, err := s.runYtDlpSearch(context.Background(), "ytsearch5:song")
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("search returned %d candidates, want 1", len(got))
	}
	assertLiveJarUntouched(t, jar)
}

func TestYtDlpAudioSearcher_Download_WorksAgainstAReadOnlyCookieJar(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	jar := readOnlyLiveJar(t)

	s := NewYtDlpAudioSearcher("", jar, "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp")
	jarWritingYtDlpAt(t, s.binary, outDir)

	if _, err := s.Download(context.Background(), "https://www.youtube.com/watch?v=aaaaaaaaaaa", outDir); err != nil {
		t.Fatalf("download error: %v", err)
	}
	assertLiveJarUntouched(t, jar)
}
