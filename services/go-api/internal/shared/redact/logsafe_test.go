package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogText(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "altune-acquire-123", "track.mp3")
	tests := []struct {
		name     string
		in       string
		wantGone []string
		wantKept []string
	}{
		{
			name:     "spaced cookies flag value",
			in:       "called with --cookies /etc/altune/jar",
			wantGone: []string{"/etc/altune/jar"},
			wantKept: []string{"--cookies [REDACTED]"},
		},
		{
			name:     "equals cookies flag value",
			in:       "argv: --cookies=/srv/jar.txt -f bestaudio",
			wantGone: []string{"/srv/jar.txt"},
			wantKept: []string{"--cookies=[REDACTED]", "-f bestaudio"},
		},
		{
			name:     "relative cookie file name",
			in:       "no such file: 'cookies.txt'",
			wantGone: []string{"cookies.txt"},
		},
		{
			name:     "python errno path without cookie in name",
			in:       "ERROR: [Errno 2] No such file or directory: '/home/deploy/secrets/yt'",
			wantGone: []string{"/home/deploy", "secrets/yt"},
			wantKept: []string{"[Errno 2] No such file or directory"},
		},
		{
			name:     "windows cookie path",
			in:       `open C:\Users\ops\cookies.txt: denied`,
			wantGone: []string{`C:\Users`},
		},
		{
			name:     "yt-dlp sign-in hint keeps its guidance and faq url",
			in:       "Sign in to confirm. Use --cookies-from-browser or --cookies for the authentication. See https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp",
			wantKept: []string{"--cookies for the authentication", "https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp"},
		},
		{
			name:     "url secrets scrubbed like httptrace",
			in:       `Get "https://api.example.com/v1?api_key=abc123&q=x": timeout`,
			wantGone: []string{"abc123"},
			wantKept: []string{"api_key=REDACTED", "q=x"},
		},
		{
			name:     "temp scratch path kept for triage",
			in:       "no mp3 file produced in " + tmpFile,
			wantKept: []string{tmpFile},
		},
		{
			name:     "python attribute access is not a cookie file",
			in:       "    self.cookiejar.save()",
			wantKept: []string{"self.cookiejar.save()"},
		},
		{
			name:     "path glued to a key with equals",
			in:       "cookiefile=/run/secrets/yt loaded",
			wantGone: []string{"/run/secrets"},
		},
		{
			name:     "file url",
			in:       "cannot load file:///srv/private/jar.txt",
			wantGone: []string{"/srv/private"},
		},
		{
			name:     "quoted path with spaces",
			in:       `open "/Volumes/ops share/secret dir/yt.txt": denied`,
			wantGone: []string{"/Volumes", "secret dir/yt.txt"},
		},
		{
			name:     "temp dir traversal is not exempt",
			in:       "open " + os.TempDir() + "/../etc/shadow",
			wantGone: []string{"etc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogText(tt.in)
			for _, gone := range tt.wantGone {
				if strings.Contains(got, gone) {
					t.Errorf("LogText(%q) = %q, still contains %q", tt.in, got, gone)
				}
			}
			for _, kept := range tt.wantKept {
				if !strings.Contains(got, kept) {
					t.Errorf("LogText(%q) = %q, lost %q", tt.in, got, kept)
				}
			}
		})
	}
}

func TestLogError_Nil(t *testing.T) {
	if got := LogError(nil); got != "" {
		t.Fatalf("LogError(nil) = %q, want empty", got)
	}
}
