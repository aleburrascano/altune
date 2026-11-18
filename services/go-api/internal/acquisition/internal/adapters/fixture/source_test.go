package fixture_test

import (
	"altune/go-api/internal/acquisition"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	unknownLengthSeconds = 180
	maxClipRepeats       = 1200
	uploadURL            = "https://fixture.invalid/upload.mp3"
	uploadChannel        = "Fixture Uploads"
)

func fetchedSize(t *testing.T, seconds float64) int {
	t.Helper()
	outDir := t.TempDir()
	fetched, err := acquisition.NewArtTrackSource().Fetch(context.Background(), acquisition.AudioCandidate{Duration: seconds}, outDir)
	if err != nil {
		t.Fatalf("Fetch(%v): %v", seconds, err)
	}
	info, err := os.Stat(fetched.Path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Size())
}

func TestSource_FetchTilesTheClipToCoverTheDurationWithinBounds(t *testing.T) {
	clip := fetchedSize(t, 0)
	if clip == 0 {
		t.Fatal("a fetch must write at least one clip")
	}
	cases := []struct {
		seconds float64
		repeats int
	}{
		{seconds: -30, repeats: 1},
		{seconds: math.NaN(), repeats: 1},
		{seconds: 0.5, repeats: 1},
		{seconds: 212, repeats: 206},
		{seconds: math.Inf(1), repeats: maxClipRepeats},
		{seconds: 1e9, repeats: maxClipRepeats},
	}
	for _, c := range cases {
		if got := fetchedSize(t, c.seconds); got != c.repeats*clip {
			t.Errorf("Fetch(%v) wrote %d bytes, want %d clips of %d", c.seconds, got, c.repeats, clip)
		}
	}
}

func TestSource_FetchWritesDirectlyInOutDir(t *testing.T) {
	outDir := t.TempDir()
	fetched, err := acquisition.NewArtTrackSource().Fetch(context.Background(), acquisition.AudioCandidate{Duration: 212}, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(fetched.Path) != outDir {
		t.Fatalf("path %q not directly inside %q", fetched.Path, outDir)
	}
}

func TestSource_FetchRefusesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outDir := t.TempDir()
	if _, err := acquisition.NewArtTrackSource().Fetch(ctx, acquisition.AudioCandidate{}, outDir); err == nil {
		t.Fatal("fetch on a cancelled context must fail")
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Fatalf("cancelled fetch wrote %d files", len(entries))
	}
}

type answeringSongs []acquisition.YtmSong

func (a answeringSongs) SearchSongs(context.Context, string) ([]acquisition.YtmSong, error) {
	return a, nil
}

func prodArtTrack(t *testing.T, req acquisition.FindRequest, song acquisition.YtmSong) acquisition.AudioCandidate {
	t.Helper()
	return findOne(t, acquisition.NewYtmSearchSource(answeringSongs{song}, nil), req)
}

func fakeYtDlpPrinting(t *testing.T, entry map[string]any) {
	t.Helper()
	line, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	payload := filepath.Join(dir, "entry.json")
	if err := os.WriteFile(payload, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat '" + payload + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func prodUpload(t *testing.T, req acquisition.FindRequest, entry map[string]any) acquisition.AudioCandidate {
	t.Helper()
	fakeYtDlpPrinting(t, entry)
	source := acquisition.NewYtDlpSource(acquisition.NewYtDlpAudioSearcher("", "", ""))
	found, err := source.Find(context.Background(), req)
	if err != nil || len(found) == 0 {
		t.Fatalf("yt-dlp source found %v err %v, want a candidate", found, err)
	}
	return found[0]
}

func TestFind_FixtureCandidatesAreShapedLikeProd(t *testing.T) {
	req := acquisition.FindRequest{Title: "Song", Artist: "Artist", Album: "Album", Duration: 179.6}

	art := findOne(t, acquisition.NewArtTrackSource(), req)
	wantArt := prodArtTrack(t, req, acquisition.YtmSong{Type: acquisition.VideoTypeATV, Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationSeconds: 180})
	wantArt.URL = acquisition.ArtTrackURL
	if !reflect.DeepEqual(art, wantArt) {
		t.Fatalf("art track %+v, want %+v", art, wantArt)
	}
	if art.Kind != acquisition.KindArtTrack || !art.IsResolved() || art.Channel != "" {
		t.Fatalf("art track %+v must be a resolved art_track with no channel", art)
	}

	upload := findOne(t, acquisition.NewUploadSource(), req)
	wantUpload := prodUpload(t, req, map[string]any{"title": "Song", "duration": 180, "webpage_url": uploadURL, "channel": uploadChannel})
	if !reflect.DeepEqual(upload, wantUpload) {
		t.Fatalf("upload %+v, want %+v", upload, wantUpload)
	}
	if upload.Kind != acquisition.KindUpload || upload.IsResolved() || upload.Channel == "" || upload.Channel == req.Artist {
		t.Fatalf("upload %+v must be an unresolved upload from a neutral channel", upload)
	}
	if acquisition.NewArtTrackSource().Name() == acquisition.NewUploadSource().Name() || art.URL == upload.URL {
		t.Fatal("the two fixture sources must differ in name and URL")
	}
}

func TestFind_UnknownDurationBecomesTheDefaultLength(t *testing.T) {
	for _, in := range []float64{0, -3, math.NaN(), math.Inf(1)} {
		req := acquisition.FindRequest{Title: "Song", Artist: "Artist", Duration: in}
		for _, source := range []acquisition.AudioSource{acquisition.NewArtTrackSource(), acquisition.NewUploadSource()} {
			if got := findOne(t, source, req).Duration; got != unknownLengthSeconds {
				t.Errorf("%s: duration %v became %v, want %d", source.Name(), in, got, unknownLengthSeconds)
			}
		}
	}
}

func findOne(t *testing.T, s acquisition.AudioSource, req acquisition.FindRequest) acquisition.AudioCandidate {
	t.Helper()
	found, err := s.Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("found %v err %v, want one candidate", found, err)
	}
	return found[0]
}
