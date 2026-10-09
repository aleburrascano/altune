package discoverytest_test

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"sync"
	"testing"
)

func TestAlbumContentProvider_ZeroValueReturnsNothing(t *testing.T) {
	got, err := (&discoverytest.AlbumContentProvider{}).GetAlbumTracks(context.Background(), domain.ProviderDeezer, "1")

	if got != nil || err != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestAlbumContentProvider_ReturnsCannedResults(t *testing.T) {
	want := []domain.SearchResult{{Title: "a"}}
	p := &discoverytest.AlbumContentProvider{Results: want}

	got, err := p.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "1")

	if err != nil || len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("got (%v, %v), want the canned result", got, err)
	}
}

func TestAlbumContentProvider_FuncOverrideWinsOverCannedResults(t *testing.T) {
	p := &discoverytest.AlbumContentProvider{
		Results: []domain.SearchResult{{Title: "canned"}},
		GetAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{{Title: "override"}}, nil
		},
	}

	got, _ := p.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "1")

	if len(got) != 1 || got[0].Title != "override" {
		t.Fatalf("got %v, want the override result", got)
	}
}

func TestAlbumContentProvider_ErrorSetBetweenCallsFailsOnlyTheSecond(t *testing.T) {
	boom := errors.New("boom")
	p := &discoverytest.AlbumContentProvider{Results: []domain.SearchResult{{Title: "a"}}}

	_, first := p.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "1")
	p.Err = boom
	_, second := p.GetAlbumTracks(context.Background(), domain.ProviderDeezer, "1")

	if first != nil || !errors.Is(second, boom) {
		t.Fatalf("errors = (%v, %v), want (nil, boom)", first, second)
	}
}

func TestArtworkResolver_CountsConcurrentCallsExactly(t *testing.T) {
	r := &discoverytest.ArtworkResolver{URL: "u"}
	if r.Calls() != 0 {
		t.Fatalf("calls before use = %d, want 0", r.Calls())
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = r.ResolveTagged(context.Background(), domain.ResultKindTrack, "t", "s", "")
		}()
	}
	wg.Wait()

	if r.Calls() != 10 {
		t.Fatalf("calls after 10 concurrent = %d, want 10", r.Calls())
	}
}
