package ytdlp_test

import (
	"altune/go-api/internal/download"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var searchSpec = regexp.MustCompile(`^\w+search\d*:(.+)$`)

func fakeYtDlpLoggingArgs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + log + "'; done\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func searchedQueries(t *testing.T, req download.FindRequest) []string {
	t.Helper()
	log := fakeYtDlpLoggingArgs(t)
	source := download.NewYtDlpSource(download.NewYtDlpAudioSearcher("", "", ""))
	if _, err := source.Find(context.Background(), req); err != nil {
		t.Fatalf("Find: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read yt-dlp args: %v", err)
	}
	var queries []string
	for _, arg := range strings.Split(string(raw), "\n") {
		if m := searchSpec.FindStringSubmatch(arg); m != nil && !slices.Contains(queries, m[1]) {
			queries = append(queries, m[1])
		}
	}
	return queries
}

func TestFind_SearchesYtDlpWithTheTracksQueries(t *testing.T) {
	tests := []struct {
		name        string
		req         download.FindRequest
		wantQueries []string
		wantMin     int
	}{
		{
			name: "basic track produces title+artist queries",
			req:  download.FindRequest{Title: "Blinding Lights", Artist: "The Weeknd"},
			wantQueries: []string{
				"Blinding Lights The Weeknd",
				"Blinding Lights The Weeknd audio",
			},
			wantMin: 2,
		},
		{
			name: "track with album adds album query",
			req:  download.FindRequest{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding"},
			wantQueries: []string{
				"Circles Post Malone",
				"Circles Post Malone Hollywood's Bleeding",
				"Circles Post Malone audio",
			},
			wantMin: 3,
		},
		{
			name: "track with ISRC includes ISRC as first query",
			req:  download.FindRequest{Title: "HUMBLE.", Artist: "Kendrick Lamar", ISRC: "USUM71700626"},
			wantQueries: []string{
				"USUM71700626",
				"HUMBLE. Kendrick Lamar",
			},
			wantMin: 3,
		},
		{
			name:    "empty album skips album query",
			req:     download.FindRequest{Title: "Song", Artist: "Artist", Album: ""},
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := searchedQueries(t, tt.req)
			if len(queries) < tt.wantMin {
				t.Errorf("expected at least %d queries, got %d: %v", tt.wantMin, len(queries), queries)
			}
			for _, want := range tt.wantQueries {
				if !slices.Contains(queries, want) {
					t.Errorf("expected query %q in %v", want, queries)
				}
			}
		})
	}
}
