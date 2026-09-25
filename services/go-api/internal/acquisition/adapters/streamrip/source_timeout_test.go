package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetch_TimeoutUnderLiveParentIsSourceUnavailable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rip")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := NewSource("deezer").WithBinary(bin)
	src.fetchTimeout = 100 * time.Millisecond

	_, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://www.deezer.com/track/1"}, dir)

	if !ports.IsSourceUnavailable(err) {
		t.Fatalf("Fetch err = %v, want a source-unavailable error", err)
	}
}
