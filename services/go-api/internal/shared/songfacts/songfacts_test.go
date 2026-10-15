package songfacts

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCanonicalISRC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"dashed", "US-CA2-10-02701", "USCA21002701"},
		{"spaced", "USCA2 1002701", "USCA21002701"},
		{"lowercase padded", "  usca21002701 ", "USCA21002701"},
		{"blank", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanonicalISRC(tt.input); got != tt.want {
				t.Fatalf("CanonicalISRC(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsCanonicalISRC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"canonical", "USCA21002701", true},
		{"dashed is not yet canonical", "US-CA2-10-02701", false},
		{"lowercase", "usca21002701", false},
		{"too short", "USCA2100270", false},
		{"too long", "USCA210027011", false},
		{"symbol inside", "USCA2100270!", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCanonicalISRC(tt.input); got != tt.want {
				t.Fatalf("IsCanonicalISRC(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNameKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ampersand reads as and", "Simon & Garfunkel", "simonandgarfunkel"},
		{"and spelled out", "Simon and Garfunkel", "simonandgarfunkel"},
		{"symbol inside a name", "Ken Car$on", "kencaron"},
		{"colon subtitle", "CALL ME IF YOU GET LOST: The Estate Sale", "callmeifyougetlostthe" + "estatesale"},
		{"letters of any script", "Café Tacvba", "cafétacvba"},
		{"only punctuation", "?!", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NameKey(tt.input); got != tt.want {
				t.Fatalf("NameKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
	if NameKey("Simon & Garfunkel") != NameKey("Simon and Garfunkel") {
		t.Fatal("ampersand and 'and' must give one name key")
	}
}

func TestSplitCredit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"ampersand", "Simon & Garfunkel", []string{"Simon", "Garfunkel"}},
		{"feat with dot", "Tyler, The Creator feat. Ken Car$on", []string{"Tyler", "The Creator", "Ken Car$on"}},
		{"spaced slash", "Drake / Future", []string{"Drake", "Future"}},
		{"slash inside a name", "AC/DC", []string{"AC/DC"}},
		{"spaced plus splits", "NiteFreak + Emmanuel Jal", []string{"NiteFreak", "Emmanuel Jal"}},
		{"unspaced plus stays inside a name", "Ice+Fire", []string{"Ice+Fire"}},
		{"x collaboration", "Playboi Carti x Ken Car$on", []string{"Playboi Carti", "Ken Car$on"}},
		{"feat inside a word stays whole", "Featurette Orchestra", []string{"Featurette Orchestra"}},
		{"single artist", "Ken Car$on", []string{"Ken Car$on"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitCredit(tt.input); !slices.Equal(got, tt.want) {
				t.Fatalf("SplitCredit(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBracketEditionSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"deluxe edition", "Donda - Deluxe Edition", "Donda (Deluxe Edition)"},
		{"original version", "Help! - Original Version", "Help! (Original Version)"},
		{"bonus track", "Graduation - Bonus Track", "Graduation (Bonus Track)"},
		{"not an edition", "Song - Remix Interlude", "Song - Remix Interlude"},
		{"no suffix", "CALL ME IF YOU GET LOST: The Estate Sale", "CALL ME IF YOU GET LOST: The Estate Sale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BracketEditionSuffix(tt.input); got != tt.want {
				t.Fatalf("BracketEditionSuffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsEditionLabel(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Deluxe", true},
		{"Deluxe Edition", true},
		{"Bonus Track", true},
		{"Original Version", true},
		{"Mono", true},
		{"Version", false},
		{"Live at Wembley", false},
		{"Estate Sale", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsEditionLabel(tt.input); got != tt.want {
				t.Fatalf("IsEditionLabel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

var copiedSongFactDefinitionRe = regexp.MustCompile(`func (?:canonicalISRC|normalizeName|isCanonicalISRC)\(`)

var compiledPatternLiteralRe = regexp.MustCompile("regexp\\.MustCompile\\(`([^`]*)`\\)")

func hasEditionWordPattern(source []byte) bool {
	for _, literal := range compiledPatternLiteralRe.FindAllSubmatch(source, -1) {
		pattern := strings.ToLower(string(literal[1]))
		if strings.Contains(pattern, "deluxe") && strings.Contains(pattern, "edition") {
			return true
		}
	}
	return false
}

func TestNoSongFactIsDefinedOutsideThePackage(t *testing.T) {
	moduleRoot := filepath.Join("..", "..", "..")
	ownDir := filepath.Join(moduleRoot, "internal", "shared", "songfacts")
	var copies []string
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == ownDir || entry.Name() == "node_modules" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if copiedSongFactDefinitionRe.Match(source) {
			copies = append(copies, path+": defines a song fact function that songfacts owns")
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !strings.HasSuffix(path, "_test.go") && hasEditionWordPattern(source) {
			copies = append(copies, path+": keeps its own edition word list")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) > 0 {
		t.Fatalf("song facts live in internal/shared/songfacts only:\n%s", strings.Join(copies, "\n"))
	}
}
