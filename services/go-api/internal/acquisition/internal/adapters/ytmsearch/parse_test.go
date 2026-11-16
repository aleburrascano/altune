package ytmsearch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestParseWatch_ReadsTypeTitleArtistsAndExplicitBadge(t *testing.T) {
	tests := map[string]struct {
		videoID string
		want    Watch
	}{
		"official audio is ATV and explicit": {
			videoID: "XDC3uH1FSgw",
			want:    Watch{VideoID: "XDC3uH1FSgw", Title: "remember them days", Artists: []string{"slayr"}, Type: VideoTypeATV, Explicit: true},
		},
		"music video is OMV and carries no badge": {
			videoID: "u4GAeAYGAqE",
			want:    Watch{VideoID: "u4GAeAYGAqE", Title: "remember them days", Artists: []string{"slayr"}, Type: VideoTypeOMV},
		},
		"clean edit is ATV without the badge": {
			videoID: "88oFlVnMEsc",
			want:    Watch{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: VideoTypeATV},
		},
		"an id YouTube Music redirects to its canonical track": {
			videoID: "B2alaIE1f5A",
			want:    Watch{VideoID: "OFqIZypoMBY", Title: "New Life", Artists: []string{"Lucki", "Chuck L.I."}, Type: VideoTypeATV, Explicit: true},
		},
		"explicit master is ATV with the badge": {
			videoID: "LMDLNLM6Ji8",
			want:    Watch{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: VideoTypeATV, Explicit: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseWatch(fixture(t, "next_"+tc.videoID+".json"))
			if err != nil {
				t.Fatalf("ParseWatch: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseWatch = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseWatch_ResponseWithoutATrackIsAnError(t *testing.T) {
	_, err := ParseWatch([]byte("{}"))

	if !errors.Is(err, ErrNoWatchTrack) {
		t.Fatalf("err = %v, want ErrNoWatchTrack", err)
	}
}

func TestParseWatch_NonJSONIsAnError(t *testing.T) {
	if _, err := ParseWatch([]byte("<html>consent</html>")); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseSongs_ReturnsCleanAndExplicitTwinsInRankOrder(t *testing.T) {
	songs, err := ParseSongs(fixture(t, "search_lucki_2021_vibes.json"))
	if err != nil {
		t.Fatalf("ParseSongs: %v", err)
	}
	if len(songs) < 2 {
		t.Fatalf("songs = %d, want at least the clean/explicit pair", len(songs))
	}

	wantClean := Song{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: VideoTypeATV}
	wantExplicit := Song{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: VideoTypeATV, Explicit: true}
	if !reflect.DeepEqual(songs[0], wantClean) {
		t.Errorf("songs[0] = %+v, want %+v", songs[0], wantClean)
	}
	if !reflect.DeepEqual(songs[1], wantExplicit) {
		t.Errorf("songs[1] = %+v, want %+v", songs[1], wantExplicit)
	}
}

func TestParseSongs_NonJSONIsAnError(t *testing.T) {
	if _, err := ParseSongs([]byte("not json")); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseOEmbed_ReadsTitleAndAuthor(t *testing.T) {
	tests := map[string]OEmbed{
		"XDC3uH1FSgw": {Title: "remember them days", Author: "slayr - Topic"},
		"u4GAeAYGAqE": {Title: "slayr - remember them days (OFFICIAL MUSIC VIDEO)", Author: "slayr"},
	}

	for videoID, want := range tests {
		t.Run(videoID, func(t *testing.T) {
			got, err := ParseOEmbed(fixture(t, "oembed_"+videoID+".json"))
			if err != nil {
				t.Fatalf("ParseOEmbed: %v", err)
			}
			if got != want {
				t.Fatalf("ParseOEmbed = %+v, want %+v", got, want)
			}
		})
	}
}
