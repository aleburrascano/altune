package ytmsearch_test

import (
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
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
		want    ytmsearch.Watch
	}{
		"official audio is ATV and explicit": {
			videoID: "XDC3uH1FSgw",
			want:    ytmsearch.Watch{VideoID: "XDC3uH1FSgw", Title: "remember them days", Artists: []string{"slayr"}, Type: ytmsearch.VideoTypeAudioOnly, Explicit: true},
		},
		"music video is OMV and carries no badge": {
			videoID: "u4GAeAYGAqE",
			want:    ytmsearch.Watch{VideoID: "u4GAeAYGAqE", Title: "remember them days", Artists: []string{"slayr"}, Type: ytmsearch.VideoTypeOfficialVideo},
		},
		"clean edit is ATV without the badge": {
			videoID: "88oFlVnMEsc",
			want:    ytmsearch.Watch{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: ytmsearch.VideoTypeAudioOnly},
		},
		"an id YouTube Music redirects to its canonical track": {
			videoID: "B2alaIE1f5A",
			want:    ytmsearch.Watch{VideoID: "OFqIZypoMBY", Title: "New Life", Artists: []string{"Lucki", "Chuck L.I."}, Type: ytmsearch.VideoTypeAudioOnly, Explicit: true},
		},
		"explicit master is ATV with the badge": {
			videoID: "LMDLNLM6Ji8",
			want:    ytmsearch.Watch{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Type: ytmsearch.VideoTypeAudioOnly, Explicit: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var requests []recordedRequest
			got, err := clientFor(fixtureServer(t, &requests), time.Second).Watch(context.Background(), tc.videoID)
			if err != nil {
				t.Fatalf("Watch: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Watch = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func watchAnswering(t *testing.T, body string) *ytmsearch.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return clientFor(srv, time.Second)
}

func TestClientWatch_ResponseWithoutATrackIsAnError(t *testing.T) {
	_, err := watchAnswering(t, "{}").Watch(context.Background(), "XDC3uH1FSgw")

	if !errors.Is(err, ytmsearch.ErrNoWatchTrack) {
		t.Fatalf("err = %v, want ErrNoWatchTrack", err)
	}
}

func TestClientWatch_NonJSONIsAnError(t *testing.T) {
	if _, err := watchAnswering(t, "<html>consent</html>").Watch(context.Background(), "XDC3uH1FSgw"); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseSongs_ReturnsCleanAndExplicitTwinsInRankOrder(t *testing.T) {
	songs, err := ytmsearch.ParseSongs(fixture(t, "search_lucki_2021_vibes.json"))
	if err != nil {
		t.Fatalf("ParseSongs: %v", err)
	}
	if len(songs) < 2 {
		t.Fatalf("songs = %d, want at least the clean/explicit pair", len(songs))
	}

	wantClean := ytmsearch.Song{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: ytmsearch.VideoTypeAudioOnly}
	wantExplicit := ytmsearch.Song{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Artists: []string{"LUCKI"}, Album: "s*x m*ney dr*gs", DurationSeconds: 125, Type: ytmsearch.VideoTypeAudioOnly, Explicit: true}
	if !reflect.DeepEqual(songs[0], wantClean) {
		t.Errorf("songs[0] = %+v, want %+v", songs[0], wantClean)
	}
	if !reflect.DeepEqual(songs[1], wantExplicit) {
		t.Errorf("songs[1] = %+v, want %+v", songs[1], wantExplicit)
	}
}

func TestParseSongs_NonJSONIsAnError(t *testing.T) {
	if _, err := ytmsearch.ParseSongs([]byte("not json")); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestParseOEmbed_ReadsTitleAndAuthor(t *testing.T) {
	tests := map[string]ytmsearch.OEmbed{
		"XDC3uH1FSgw": {Title: "remember them days", Author: "slayr - Topic"},
		"u4GAeAYGAqE": {Title: "slayr - remember them days (OFFICIAL MUSIC VIDEO)", Author: "slayr"},
	}

	for videoID, want := range tests {
		t.Run(videoID, func(t *testing.T) {
			got, err := ytmsearch.ParseOEmbed(fixture(t, "oembed_"+videoID+".json"))
			if err != nil {
				t.Fatalf("ParseOEmbed: %v", err)
			}
			if got != want {
				t.Fatalf("ParseOEmbed = %+v, want %+v", got, want)
			}
		})
	}
}

func seedFromFixtures(f *testing.F, pattern string) {
	f.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", pattern))
	if err != nil || len(paths) == 0 {
		f.Fatalf("no fixtures match %q: %v", pattern, err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("read fixture: %v", err)
		}
		f.Add(raw)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
}

func assertParsesTheSameTwice[T any](t *testing.T, parse func([]byte) (T, error), raw []byte) {
	t.Helper()
	first, firstErr := parse(raw)
	second, secondErr := parse(raw)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("parsing %q twice gave %+v then %+v", raw, first, second)
	}
	if (firstErr == nil) != (secondErr == nil) || (firstErr != nil && firstErr.Error() != secondErr.Error()) {
		t.Fatalf("parsing %q twice failed differently: %v then %v", raw, firstErr, secondErr)
	}
}

func FuzzParseWatch(f *testing.F) {
	seedFromFixtures(f, "next_*.json")
	f.Fuzz(func(t *testing.T, raw []byte) {
		assertParsesTheSameTwice(t, ytmsearch.ParseWatch, raw)
	})
}

func FuzzParseSongs(f *testing.F) {
	seedFromFixtures(f, "search_*.json")
	f.Fuzz(func(t *testing.T, raw []byte) {
		assertParsesTheSameTwice(t, ytmsearch.ParseSongs, raw)
	})
}

func FuzzParseOEmbed(f *testing.F) {
	seedFromFixtures(f, "oembed_*.json")
	f.Fuzz(func(t *testing.T, raw []byte) {
		assertParsesTheSameTwice(t, ytmsearch.ParseOEmbed, raw)
	})
}
