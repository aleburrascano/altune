package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"strings"
	"testing"
)

import (
	"errors"
	"reflect"
	"sync"
	"time"
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

func TestSource_Find_RunsEveryQueryEvenAfterEnoughCandidatesAreFound(t *testing.T) {
	var mu sync.Mutex
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		return candidatesPerSpec(spec, 5), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantSearches := len(ports.SearchQueries(findRequest())) * len(searchEngines)
	if len(specs) != wantSearches {
		t.Fatalf("searches = %d %v, want all %d even though the first query's engines already return %d candidates",
			len(specs), specs, wantSearches, ports.EnoughCandidates)
	}
	if len(got) != wantSearches*5 {
		t.Fatalf("merged candidates = %d, want %d", len(got), wantSearches*5)
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
	var mu sync.Mutex
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
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

func concurrencySafeRunner(run func(spec string) ([]ports.AudioCandidate, error)) searchRunner {
	var mu sync.Mutex
	return func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		defer mu.Unlock()
		return run(spec)
	}
}

func TestSource_Find_MustHold9_ISRCQueryWithManyHitsStillRunsEveryOtherQueryVariant(t *testing.T) {
	req := findRequest()
	seen := map[string]bool{}
	src := NewSource(withRunner(concurrencySafeRunner(func(spec string) ([]ports.AudioCandidate, error) {
		seen[spec] = true
		if strings.Contains(spec, req.ISRC) {
			return candidatesPerSpec(spec, 10), nil
		}
		return candidatesPerSpec(spec, 1), nil
	})))

	got, err := src.Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantQueries := ports.SearchQueries(req)
	if len(wantQueries) < 4 {
		t.Fatalf("findRequest fixture carries only %d query variants, want at least 4 to prove this", len(wantQueries))
	}
	for _, query := range wantQueries {
		for _, engine := range searchEngines {
			if !seen[engine+query] {
				t.Fatalf("spec %q never ran, want the ISRC hit count to never suppress another query variant", engine+query)
			}
		}
	}

	wantCandidates := 10*len(searchEngines) + (len(wantQueries)-1)*len(searchEngines)
	if len(got) != wantCandidates {
		t.Fatalf("merged candidates = %d, want %d (the ISRC hits on both engines plus one per other query/engine pair)", len(got), wantCandidates)
	}
}

func TestSource_Find_NeverRunsMoreThanFourSearchesAtOnce(t *testing.T) {
	var mu sync.Mutex
	current, maxSeen := 0, 0
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		current++
		if current > maxSeen {
			maxSeen = current
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()

		return candidatesPerSpec(spec, 1), nil
	}))

	if _, err := src.Find(context.Background(), findRequest()); err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if maxSeen > maxConcurrentSearches {
		t.Fatalf("max concurrent searches observed = %d, want no more than %d", maxSeen, maxConcurrentSearches)
	}
	if maxSeen < maxConcurrentSearches {
		t.Fatalf("max concurrent searches observed = %d, want the bound of %d to actually be reached", maxSeen, maxConcurrentSearches)
	}
}

func TestSource_Find_OrderIsDeterministicAndSurvivesOneFailingSearch(t *testing.T) {
	newSrc := func() *Source {
		return NewSource(withRunner(concurrencySafeRunner(func(spec string) ([]ports.AudioCandidate, error) {
			if strings.Contains(spec, "scsearch5:") && strings.Contains(spec, "audio") {
				return nil, errors.New("soundcloud blew up")
			}
			return candidatesPerSpec(spec, 1), nil
		})))
	}

	first, err := newSrc().Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	second, err := newSrc().Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(first) == 0 {
		t.Fatal("Find returned no candidates despite only one of eight searches failing")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Find order changed across runs with identical fake responses:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}
