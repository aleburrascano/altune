package ytmsearch_test

import (
	"altune/go-api/internal/acquisition"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestClientWatch_ReadsTypeTitleArtistsAndExplicitBadge(t *testing.T) {
	tests := map[string]struct {
		videoID string
		want    acquisition.YtmWatch
	}{
		"official audio is ATV and explicit": {
			videoID: "XDC3uH1FSgw",
			want:    acquisition.YtmWatch{VideoID: "XDC3uH1FSgw", Title: "remember them days", Artists: []string{"slayr"}, Type: acquisition.VideoTypeATV, Explicit: true},
		},
		"music video is OMV and carries no badge": {
			videoID: "u4GAeAYGAqE",
			want:    acquisition.YtmWatch{VideoID: "u4GAeAYGAqE", Title: "remember them days", Artists: []string{"slayr"}, Type: acquisition.VideoTypeOMV},
		},
		"clean edit is ATV without the badge": {
			videoID: "88oFlVnMEsc",
			want:    acquisition.YtmWatch{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: acquisition.VideoTypeATV},
		},
		"an id YouTube Music redirects to its canonical track": {
			videoID: "B2alaIE1f5A",
			want:    acquisition.YtmWatch{VideoID: "OFqIZypoMBY", Title: "New Life", Artists: []string{"Lucki", "Chuck L.I."}, Type: acquisition.VideoTypeATV, Explicit: true},
		},
		"explicit master is ATV with the badge": {
			videoID: "LMDLNLM6Ji8",
			want:    acquisition.YtmWatch{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: acquisition.VideoTypeATV, Explicit: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var requests []recordedRequest
			got, err := clientFor(fixtureServer(t, &requests), 0, time.Second).Watch(context.Background(), tc.videoID)
			if err != nil {
				t.Fatalf("Watch: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Watch = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func watchAnswering(t *testing.T, body string) *acquisition.YtmSearchClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return clientFor(srv, 0, time.Second)
}

func TestClientWatch_ResponseWithoutATrackIsAnError(t *testing.T) {
	_, err := watchAnswering(t, "{}").Watch(context.Background(), "XDC3uH1FSgw")

	if !errors.Is(err, acquisition.ErrNoWatchTrack) {
		t.Fatalf("err = %v, want ErrNoWatchTrack", err)
	}
}

func TestClientWatch_NonJSONIsAnError(t *testing.T) {
	if _, err := watchAnswering(t, "<html>consent</html>").Watch(context.Background(), "XDC3uH1FSgw"); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseSongs_ReturnsCleanAndExplicitTwinsInRankOrder(t *testing.T) {
	songs, err := acquisition.ParseYtmSongs(fixture(t, "search_lucki_2021_vibes.json"))
	if err != nil {
		t.Fatalf("ParseSongs: %v", err)
	}
	if len(songs) < 2 {
		t.Fatalf("songs = %d, want at least the clean/explicit pair", len(songs))
	}

	wantClean := acquisition.YtmSong{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: acquisition.VideoTypeATV}
	wantExplicit := acquisition.YtmSong{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: acquisition.VideoTypeATV, Explicit: true}
	if !reflect.DeepEqual(songs[0], wantClean) {
		t.Errorf("songs[0] = %+v, want %+v", songs[0], wantClean)
	}
	if !reflect.DeepEqual(songs[1], wantExplicit) {
		t.Errorf("songs[1] = %+v, want %+v", songs[1], wantExplicit)
	}
}

func TestParseSongs_NonJSONIsAnError(t *testing.T) {
	if _, err := acquisition.ParseYtmSongs([]byte("not json")); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseOEmbed_ReadsTitleAndAuthor(t *testing.T) {
	tests := map[string]acquisition.YtmOEmbed{
		"XDC3uH1FSgw": {Title: "remember them days", Author: "slayr - Topic"},
		"u4GAeAYGAqE": {Title: "slayr - remember them days (OFFICIAL MUSIC VIDEO)", Author: "slayr"},
	}

	for videoID, want := range tests {
		t.Run(videoID, func(t *testing.T) {
			got, err := acquisition.ParseYtmOEmbed(fixture(t, "oembed_"+videoID+".json"))
			if err != nil {
				t.Fatalf("ParseOEmbed: %v", err)
			}
			if got != want {
				t.Fatalf("ParseOEmbed = %+v, want %+v", got, want)
			}
		})
	}
}
