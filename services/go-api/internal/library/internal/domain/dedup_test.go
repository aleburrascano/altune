package domain_test

import (
	"altune/go-api/internal/library"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestComputeDedupKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		title  string
		artist string
		album  string
		want   string
	}{
		{
			name:   "basic case",
			title:  "Hello",
			artist: "Adele",
			album:  "25",
			want:   "hello|adele|25",
		},
		{
			name:   "case insensitive",
			title:  "HELLO",
			artist: "ADELE",
			album:  "TWENTY FIVE",
			want:   "hello|adele|twenty five",
		},
		{
			name:   "whitespace normalized",
			title:  "  Hello   World  ",
			artist: "  Some   Artist  ",
			album:  "  The   Album  ",
			want:   "hello world|some artist|the album",
		},
		{
			name:   "special chars stripped",
			title:  "Hello! (World) [Remix]",
			artist: "Artist & Co.",
			album:  "Album #1",
			want:   "hello world remix|artist co|album 1",
		},
		{
			name:   "all fields empty",
			title:  "",
			artist: "",
			album:  "",
			want:   "||",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := library.TrackDedupKey(tt.title, tt.artist, tt.album)
			if got != tt.want {
				t.Errorf("TrackDedupKey(%q, %q, %q) = %q, want %q",
					tt.title, tt.artist, tt.album, got, tt.want)
			}
		})
	}
}

func TestTrackDedupKey_AnEmptyAlbumKeysAsTheTitleLikeANewTrack(t *testing.T) {
	t.Parallel()
	if got := library.TrackDedupKey("Song", "Artist", ""); got != "song|artist|song" {
		t.Errorf("empty album: %q", got)
	}
	if got := library.TrackDedupKey("Song", "Artist", "  LP "); got != "song|artist|lp" {
		t.Errorf("album LP: %q", got)
	}
}

func asciiWord() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z0-9]{1,12}`)
}

func TestTrackDedupKey_AlwaysHasThreeParts(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		key := library.TrackDedupKey(rapid.String().Draw(t, "title"), rapid.String().Draw(t, "artist"), rapid.String().Draw(t, "album"))

		if got := strings.Count(key, "|"); got != 2 {
			t.Fatalf("key %q has %d separators, want 2", key, got)
		}
	})
}

func TestTrackDedupKey_IgnoresCaseAndSurroundingSpace(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		title, artist, album := asciiWord().Draw(t, "title"), asciiWord().Draw(t, "artist"), asciiWord().Draw(t, "album")

		plain := library.TrackDedupKey(title, artist, album)
		shouted := library.TrackDedupKey("  "+strings.ToUpper(title)+" ", strings.ToLower(artist), " "+strings.ToUpper(album))

		if plain != shouted {
			t.Fatalf("keys differ: %q vs %q", plain, shouted)
		}
	})
}

func TestTrackDedupKey_NormalizingTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		key := library.TrackDedupKey(rapid.String().Draw(t, "title"), rapid.String().Draw(t, "artist"), rapid.String().Draw(t, "album"))
		parts := strings.Split(key, "|")
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			t.Skip("an empty part is refilled from the title")
		}

		if again := library.TrackDedupKey(parts[0], parts[1], parts[2]); again != key {
			t.Fatalf("key %q renormalised to %q", key, again)
		}
	})
}
