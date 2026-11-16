package fixture

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipRepeats_CoversDurationWithinBounds(t *testing.T) {
	cases := []struct {
		seconds float64
		want    int
	}{
		{seconds: 0, want: 1},
		{seconds: -30, want: 1},
		{seconds: math.NaN(), want: 1},
		{seconds: 0.5, want: 1},
		{seconds: 212, want: 206},
		{seconds: math.Inf(1), want: maxClipRepeats},
		{seconds: 1e9, want: maxClipRepeats},
	}
	for _, c := range cases {
		if got := clipRepeats(c.seconds); got != c.want {
			t.Errorf("clipRepeats(%v) = %d, want %d", c.seconds, got, c.want)
		}
	}
}

func TestSource_FetchWritesTiledClipDirectlyInOutDir(t *testing.T) {
	outDir := t.TempDir()
	fetched, err := NewArtTrackSource().Fetch(context.Background(), ports.AudioCandidate{Duration: 212}, outDir)
	path := fetched.Path
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != outDir {
		t.Fatalf("path %q not directly inside %q", path, outDir)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 206*len(clip) {
		t.Fatalf("wrote %d bytes, want %d", len(written), 206*len(clip))
	}
}

func TestSource_FetchRefusesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outDir := t.TempDir()
	if _, err := NewArtTrackSource().Fetch(ctx, ports.AudioCandidate{}, outDir); err == nil {
		t.Fatal("fetch on a cancelled context must fail")
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Fatalf("cancelled fetch wrote %d files", len(entries))
	}
}

func TestFind_FixtureCandidatesAreShapedLikeProd(t *testing.T) {
	req := ports.FindRequest{Title: "Song", Artist: "Artist", Album: "Album", Duration: 179.6}

	art := findOne(t, NewArtTrackSource(), req)
	wantArt := ytmsearch.CandidateFor(ytmsearch.Song{Type: ytmsearch.VideoTypeATV, Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationSeconds: 180})
	wantArt.URL = ArtTrackURL
	if !reflect.DeepEqual(art, wantArt) {
		t.Fatalf("art track %+v, want %+v", art, wantArt)
	}
	if art.Kind != ports.KindArtTrack || !art.IsResolved() || art.Channel != "" {
		t.Fatalf("art track %+v must be a resolved art_track with no channel", art)
	}

	upload := findOne(t, NewUploadSource(), req)
	wantUpload := ytdlp.CandidateFromEntry(ytdlp.Entry{Title: "Song", Duration: 180, WebpageURL: UploadURL, Channel: uploadChannel})
	if !reflect.DeepEqual(upload, wantUpload) {
		t.Fatalf("upload %+v, want %+v", upload, wantUpload)
	}
	if upload.Kind != ports.KindUpload || upload.IsResolved() || upload.Channel == req.Artist {
		t.Fatalf("upload %+v must be an unresolved upload from a neutral channel", upload)
	}
	if NewArtTrackSource().Name() == NewUploadSource().Name() || art.URL == upload.URL {
		t.Fatal("the two fixture sources must differ in name and URL")
	}
}

func TestFixtureLength_UnknownBecomesDefault(t *testing.T) {
	for _, in := range []float64{0, -3, math.NaN(), math.Inf(1)} {
		if got := fixtureLength(in); got != unknownLength {
			t.Errorf("fixtureLength(%v) = %d, want %d", in, got, unknownLength)
		}
	}
}

func findOne(t *testing.T, s *Source, req ports.FindRequest) ports.AudioCandidate {
	t.Helper()
	found, err := s.Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("found %v err %v, want one candidate", found, err)
	}
	return found[0]
}
