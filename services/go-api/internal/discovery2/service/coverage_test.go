package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func tracksOf(titles ...string) []domain.SearchResult {
	out := make([]domain.SearchResult, len(titles))
	for i, t := range titles {
		out[i] = domain.SearchResult{Kind: domain.ResultKindTrack, Title: t}
	}
	return out
}

func fetcherReturning(tracks []domain.SearchResult, err error, called *int) TrackFetcher {
	return func(_ context.Context) ([]domain.SearchResult, error) {
		*called++
		return tracks, err
	}
}

func TestFirstNonEmptyTracks_FallsBackWhenPrimaryEmpty(t *testing.T) {
	var primaryCalls, fallbackCalls int
	got, err := FirstNonEmptyTracks(context.Background(),
		fetcherReturning(nil, nil, &primaryCalls),
		fetcherReturning(tracksOf("A", "B"), nil, &fallbackCalls),
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tracks, want 2 (from fallback)", len(got))
	}
	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Errorf("calls primary=%d fallback=%d, want 1/1", primaryCalls, fallbackCalls)
	}
}

func TestFirstNonEmptyTracks_StopsAtFirstNonEmpty(t *testing.T) {
	var primaryCalls, fallbackCalls int
	got, _ := FirstNonEmptyTracks(context.Background(),
		fetcherReturning(tracksOf("A"), nil, &primaryCalls),
		fetcherReturning(tracksOf("B"), nil, &fallbackCalls),
	)
	if len(got) != 1 || got[0].Title != "A" {
		t.Fatalf("got %v, want primary's [A]", got)
	}
	if fallbackCalls != 0 {
		t.Errorf("fallback called %d times, want 0", fallbackCalls)
	}
}

func TestFirstNonEmptyTracks_ErrorFallsThrough(t *testing.T) {
	var c1, c2 int
	got, err := FirstNonEmptyTracks(context.Background(),
		fetcherReturning(nil, errors.New("boom"), &c1),
		fetcherReturning(tracksOf("A"), nil, &c2),
	)
	if err != nil {
		t.Fatalf("err = %v, want nil (recovered by fallback)", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
}

func TestFirstNonEmptyTracks_AllEmpty(t *testing.T) {
	var c1, c2 int
	got, err := FirstNonEmptyTracks(context.Background(),
		fetcherReturning(nil, nil, &c1),
		fetcherReturning(nil, nil, &c2),
	)
	if got != nil || err != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestFirstNonEmptyTracks_AllErrorReturnsLastError(t *testing.T) {
	var c1, c2 int
	_, err := FirstNonEmptyTracks(context.Background(),
		fetcherReturning(nil, errors.New("first"), &c1),
		fetcherReturning(nil, errors.New("last"), &c2),
	)
	if err == nil || err.Error() != "last" {
		t.Fatalf("err = %v, want \"last\"", err)
	}
}

func TestFirstNonEmptyTracks_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	_, err := FirstNonEmptyTracks(ctx, fetcherReturning(tracksOf("A"), nil, &calls))
	if err == nil {
		t.Fatal("want context error")
	}
	if calls != 0 {
		t.Errorf("fetcher called %d times after cancel, want 0", calls)
	}
}
