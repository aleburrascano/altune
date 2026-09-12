package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestFetchAlbums_surfacesProviderFetchError reproduces the gap: when a seed
// fetch fails, seedFrom produced a bare rawSeed{status:"error"} and discarded
// the underlying error entirely — never logged and never carried. ReRunDetail
// is an operator diagnostic for "why doesn't X show up", so the reason (bad
// provider id, timeout, 404) must be surfaced the way sibling fanOutRerun
// already captures it into ProviderTrace.Err, not silently dropped.
func TestFetchAlbums_surfacesProviderFetchError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dr := &detailReRunner{}
	seed := dr.fetchAlbums(context.Background(), "totally-unknown-provider", "abc123", "Artist")

	if seed.status != "error" {
		t.Fatalf("want error status for a failed fetch, got %q", seed.status)
	}
	if seed.err == "" {
		t.Errorf("provider fetch error was silently dropped: rawSeed carried no error message")
	}
	logged := buf.String()
	if !strings.Contains(logged, "abc123") {
		t.Errorf("provider fetch error was silently dropped: nothing logged at the seed call site. log=%q", logged)
	}
}

// TestProjectSeeds_surfacesErrorToOperator guards the wire surface: an errored
// seed must carry its reason into DetailSeedGroup.Error so the admin diagnostic
// can distinguish a timeout from a 404 from a bad provider id.
func TestProjectSeeds_surfacesErrorToOperator(t *testing.T) {
	seeds := []rawSeed{{provider: "deezer", externalID: "x", status: "error", err: "deezer: 503 timeout"}}
	got := projectSeeds(seeds)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].Error != "deezer: 503 timeout" {
		t.Errorf("want the fetch error surfaced to the operator, got %q", got[0].Error)
	}
}
