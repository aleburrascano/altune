package domain

import "testing"

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func TestStringExtra(t *testing.T) {
	cases := []struct {
		name   string
		extras map[string]any
		want   string
	}{
		{"nil map", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"string value", map[string]any{ExtraAlbum: "123"}, "123"},
		{"empty string", map[string]any{ExtraAlbum: ""}, ""},
		{"non-string value", map[string]any{ExtraAlbum: 123}, ""},
		{"nil value", map[string]any{ExtraAlbum: nil}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StringExtra(c.extras, ExtraAlbum); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

var removedExtraKeys = []string{
	"artwork_path", "duration", "explicit", "track_number", "disc_number",
	"track_position", "upc", "mbid", "isrc", "record_type",
}

var removedExtraKeyUse = regexp.MustCompile(
	`\bExtra(ArtworkPath|Duration|Explicit|TrackNumber|DiscNumber|TrackPosition|UPC|MBID|ISRC|RecordType)\b` +
		`|\bExtras\[\s*"(` + strings.Join(removedExtraKeys, "|") + `)"\s*\]` +
		`|\bextras\[\s*"(` + strings.Join(removedExtraKeys, "|") + `)"\s*\]` +
		`|[{,\s]"(` + strings.Join(removedExtraKeys, "|") + `)"\s*:`)

func TestDiscoveryNeverReadsOrWritesARemovedExtrasKey(t *testing.T) {
	discoveryRoot := filepath.Join("..")
	err := filepath.WalkDir(discoveryRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(source), "\n") {
			if match := removedExtraKeyUse.FindString(line); match != "" {
				t.Errorf("%s:%d uses the removed extras key %q; read the typed SearchResult field instead", path, i+1, match)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
