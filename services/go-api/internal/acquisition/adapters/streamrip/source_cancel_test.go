package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetch_ParentCancellationIsNotSourceUnavailable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rip")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := NewSource("deezer").WithBinary(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := src.Fetch(ctx, ports.AudioCandidate{URL: "https://www.deezer.com/track/1"}, dir)

	if err == nil || ports.IsSourceUnavailable(err) {
		t.Fatalf("Fetch err = %v, want a plain error", err)
	}
}
