package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"strings"
	"testing"
)

func findRequest() ports.FindRequest {
	return ports.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		ISRC:   "USUG11904206",
	}
}

func candidatesPerSpec(spec string, n int) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, n)
	for i := range n {
		out = append(out, ports.AudioCandidate{
			Title: spec,
			URL:   fmt.Sprintf("https://example.test/%s/%d", spec, i),
		})
	}
	return out
}

func TestSource_Find_StopsSearchingOnceEnoughCandidatesAreFound(t *testing.T) {
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		return candidatesPerSpec(spec, 5), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(specs) > len(searchEngines) {
		t.Fatalf("searches = %d %v, want no more than the first query's %d once %d candidates are in",
			len(specs), specs, len(searchEngines), ports.EnoughCandidates)
	}
	if len(got) < ports.EnoughCandidates {
		t.Fatalf("merged candidates = %d, want at least %d", len(got), ports.EnoughCandidates)
	}
}

// Issue #1973: the per-query failure log carried the subprocess error verbatim,
// cookie jar path included.
func TestSource_Find_QueryFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := captureLogs(t)
	src := NewSource(withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, cookieJarError()
	}))

	if _, err := src.Find(context.Background(), findRequest()); err == nil {
		t.Fatal("every query failed, Find reported no error")
	}

	logged := logs.String()
	if !strings.Contains(logged, "acquisition.search_query_failed") {
		t.Fatalf("expected the query failure log, got:\n%s", logged)
	}
	if strings.Contains(logged, "/secret") {
		t.Fatalf("the cookie jar path leaked into the log:\n%s", logged)
	}
	if !strings.Contains(logged, "exit status 1") {
		t.Fatalf("redaction dropped the diagnostic text:\n%s", logged)
	}
}

func TestSource_Find_RunsEveryQueryWhileCandidatesStayScarce(t *testing.T) {
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		return candidatesPerSpec(spec, 1), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantSearches := len(ports.SearchQueries(findRequest())) * len(searchEngines)
	if len(specs) != wantSearches {
		t.Fatalf("searches = %d %v, want all %d when no query is fruitful", len(specs), specs, wantSearches)
	}
	if len(got) != wantSearches {
		t.Fatalf("merged candidates = %d, want %d", len(got), wantSearches)
	}
}
