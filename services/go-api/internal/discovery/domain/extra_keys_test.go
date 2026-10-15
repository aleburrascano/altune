package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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

func TestSearchResult_SharedMBIDFieldStaysRemoved(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "search_result.go", nil, 0)
	if err != nil {
		t.Fatalf("parse search_result.go: %v", err)
	}
	var fields []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "SearchResult" {
			return true
		}
		for _, f := range spec.Type.(*ast.StructType).Fields.List {
			for _, name := range f.Names {
				fields = append(fields, name.Name)
			}
		}
		return false
	})
	for _, f := range fields {
		if f == "MBID" {
			t.Fatalf("SearchResult has the shared MBID field again, want typed RecordingMBID/ReleaseMBID/ReleaseGroupMBID/ArtistMBID only")
		}
	}
	if len(fields) == 0 {
		t.Fatal("SearchResult struct not found in search_result.go")
	}
}

func TestSearchResult_IdentityMBIDIsTheKindsOwnTypedField(t *testing.T) {
	typed := SearchResult{RecordingMBID: "rec", ReleaseMBID: "rel", ReleaseGroupMBID: "rg", ArtistMBID: "art"}
	cases := []struct {
		name         string
		kind         ResultKind
		result       SearchResult
		wantIdentity string
		wantEntity   string
	}{
		{"track is its recording", ResultKindTrack, typed, "rec", "rec"},
		{"album is its release group, never its release", ResultKindAlbum, typed, "rg", "rg"},
		{"artist is its artist", ResultKindArtist, typed, "art", "art"},
		{"album with only a release has no identity but keeps the release for lookups", ResultKindAlbum, SearchResult{ReleaseMBID: "rel"}, "", "rel"},
		{"track never borrows a release", ResultKindTrack, SearchResult{ReleaseMBID: "rel"}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.result.Kind = c.kind
			if got := c.result.IdentityMBID(); got != c.wantIdentity {
				t.Errorf("IdentityMBID = %q, want %q", got, c.wantIdentity)
			}
			if got := c.result.EntityMBID(); got != c.wantEntity {
				t.Errorf("EntityMBID = %q, want %q", got, c.wantEntity)
			}
		})
	}
}
