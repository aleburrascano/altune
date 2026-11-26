package fakesources_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	unknownLengthSeconds = 180
	maxClipRepeats       = 1200
	uploadURL            = "https://fixture.invalid/upload.mp3"
	uploadChannel        = "Fixture Uploads"
)

var fixtureClip = []byte("one clip of fixture audio")

func fetchedSize(t *testing.T, seconds float64) int {
	t.Helper()
	outDir := t.TempDir()
	fetched, err := download.NewArtTrackSource(fixtureClip).Fetch(context.Background(), download.AudioCandidate{Duration: seconds}, outDir)
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
	fetched, err := download.NewArtTrackSource(fixtureClip).Fetch(context.Background(), download.AudioCandidate{Duration: 212}, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(fetched.Path) != outDir {
		t.Fatalf("path %q not directly inside %q", fetched.Path, outDir)
	}
}

func TestSource_NoBinaryEmbedsAudio(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", `{{range .EmbedFiles}}{{$.ImportPath}} {{.}}{{"\n"}}{{end}}`, "altune/go-api/cmd/...").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ext := strings.ToLower(path.Ext(line)); ext == ".mp3" || ext == ".m4a" || ext == ".opus" || ext == ".wav" {
			t.Errorf("a binary embeds audio: %s, want the fixture clip passed in by tests", line)
		}
	}
}

func TestSource_FetchWritesTheClipItWasGiven(t *testing.T) {
	for _, source := range []download.AudioSource{download.NewArtTrackSource(fixtureClip), download.NewUploadSource(fixtureClip)} {
		fetched, err := source.Fetch(context.Background(), download.AudioCandidate{}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(fetched.Path)
		if err != nil {
			t.Fatal(err)
		}
		if string(written) != string(fixtureClip) {
			t.Errorf("%s wrote %q, want the clip it was given %q", source.Name(), written, fixtureClip)
		}
	}
}

func TestSource_FetchRefusesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outDir := t.TempDir()
	if _, err := download.NewArtTrackSource(fixtureClip).Fetch(ctx, download.AudioCandidate{}, outDir); err == nil {
		t.Fatal("fetch on a cancelled context must fail")
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Fatalf("cancelled fetch wrote %d files", len(entries))
	}
}

type answeringSongs []download.YouTubeMusicSong

func (a answeringSongs) SearchSongs(context.Context, string) ([]download.YouTubeMusicSong, error) {
	return a, nil
}

func prodArtTrack(t *testing.T, req download.FindRequest, song download.YouTubeMusicSong) download.AudioCandidate {
	t.Helper()
	return findOne(t, download.NewYtmSearchSource(answeringSongs{song}, nil), req)
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

func prodUpload(t *testing.T, req download.FindRequest, entry map[string]any) download.AudioCandidate {
	t.Helper()
	fakeYtDlpPrinting(t, entry)
	source := download.NewYtDlpSource(download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep))
	found, err := source.Find(context.Background(), req)
	if err != nil || len(found) == 0 {
		t.Fatalf("yt-dlp source found %v err %v, want a candidate", found, err)
	}
	return found[0]
}

func TestFind_FixtureCandidatesAreShapedLikeProd(t *testing.T) {
	req := download.FindRequest{Title: "Song", Artist: "Artist", Album: "Album", Duration: 179.6}

	art := findOne(t, download.NewArtTrackSource(fixtureClip), req)
	wantArt := prodArtTrack(t, req, download.YouTubeMusicSong{VideoID: "dQw4w9WgXcQ", Type: download.VideoTypeAudioOnly, Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationSeconds: 180})
	wantArt.URL = download.ArtTrackURL
	if !reflect.DeepEqual(art, wantArt) {
		t.Fatalf("art track %+v, want %+v", art, wantArt)
	}
	if art.Kind != download.KindArtTrack || !art.IsResolved() || art.Channel != "" {
		t.Fatalf("art track %+v must be a resolved art_track with no channel", art)
	}

	upload := findOne(t, download.NewUploadSource(fixtureClip), req)
	wantUpload := prodUpload(t, req, map[string]any{"title": "Song", "duration": 180, "webpage_url": uploadURL, "channel": uploadChannel})
	if !reflect.DeepEqual(upload, wantUpload) {
		t.Fatalf("upload %+v, want %+v", upload, wantUpload)
	}
	if upload.Kind != download.KindUpload || upload.IsResolved() || upload.Channel == "" || upload.Channel == req.Artist {
		t.Fatalf("upload %+v must be an unresolved upload from a neutral channel", upload)
	}
	if download.NewArtTrackSource(fixtureClip).Name() == download.NewUploadSource(fixtureClip).Name() || art.URL == upload.URL {
		t.Fatal("the two fixture sources must differ in name and URL")
	}
}

func TestFind_UnknownDurationBecomesTheDefaultLength(t *testing.T) {
	for _, in := range []float64{0, -3, math.NaN(), math.Inf(1)} {
		req := download.FindRequest{Title: "Song", Artist: "Artist", Duration: in}
		for _, source := range []download.AudioSource{download.NewArtTrackSource(fixtureClip), download.NewUploadSource(fixtureClip)} {
			if got := findOne(t, source, req).Duration; got != unknownLengthSeconds {
				t.Errorf("%s: duration %v became %v, want %d", source.Name(), in, got, unknownLengthSeconds)
			}
		}
	}
}

func findOne(t *testing.T, s download.AudioSource, req download.FindRequest) download.AudioCandidate {
	t.Helper()
	found, err := s.Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("found %v err %v, want one candidate", found, err)
	}
	return found[0]
}
