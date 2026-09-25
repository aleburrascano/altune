package fixture

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"math"
	"os"
	"path/filepath"
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
	path, err := NewSource().Fetch(context.Background(), ports.AudioCandidate{Duration: 212}, outDir)
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
	if _, err := NewSource().Fetch(ctx, ports.AudioCandidate{}, outDir); err == nil {
		t.Fatal("fetch on a cancelled context must fail")
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Fatalf("cancelled fetch wrote %d files", len(entries))
	}
}

func TestSource_FindOffersOneCandidateMatchingTheTrack(t *testing.T) {
	req := ports.FindRequest{Title: "Song", Artist: "Artist", Duration: 180}
	found, err := NewSource().Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("found %v err %v, want one candidate", found, err)
	}
	got := found[0]
	if got.Title != req.Title || got.Channel != req.Artist || got.Duration != req.Duration || got.URL == "" {
		t.Fatalf("candidate %+v does not mirror request %+v", got, req)
	}
}
