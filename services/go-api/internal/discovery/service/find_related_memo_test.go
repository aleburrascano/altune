package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

type countingRelationshipQuerier struct {
	matchesByUser map[shared.UserId][]ports.RelatedTrackMatch
	calls         atomic.Int64
}

func (q *countingRelationshipQuerier) FindRelatedByAlbum(_ context.Context, userId shared.UserId, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	q.calls.Add(1)
	return q.matchesByUser[userId], nil
}

type countingAlbumTracksProvider struct {
	tracksByAlbum map[string][]domain.SearchResult
	calls         atomic.Int64
}

func (p *countingAlbumTracksProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	return p.tracksByAlbum[albumID], nil
}

// relatedSearchFixture is a slate of tracks whose titles all match the query, so
// ranking keeps every one of them in the top relatedTopN, each on its own album
// so each draws its own library query and provider call.
func relatedSearchFixture() ([]domain.SearchResult, map[string][]domain.SearchResult) {
	var organic []domain.SearchResult
	tracksByAlbum := map[string][]domain.SearchResult{}
	for i := 0; i < relatedTopN; i++ {
		albumID := fmt.Sprintf("al-%d", i)
		track := trackResult(domain.ProviderDeezer, fmt.Sprintf("d-%d", i),
			fmt.Sprintf("Humble %d", i), fmt.Sprintf("Artist %d", i), nil)
		track.Album = fmt.Sprintf("Album %d", i)
		track.DeezerAlbumID = albumID
		organic = append(organic, track)
		tracksByAlbum[albumID] = []domain.SearchResult{
			trackResult(domain.ProviderDeezer, fmt.Sprintf("s-%d", i),
				fmt.Sprintf("Sibling %d", i), fmt.Sprintf("Artist %d", i), nil),
		}
	}
	return organic, tracksByAlbum
}

func libraryTitles(out *SearchOutput) []string {
	var titles []string
	for _, group := range out.Related {
		if group.Relationship != domain.RelationshipLibraryMatches {
			continue
		}
		for _, item := range group.Items {
			titles = append(titles, item.Title)
		}
	}
	return titles
}

func searchAs(t *testing.T, svc *Service, userId shared.UserId, raw string) *SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), userId, newQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_RepeatedSearch_ServesRelatedGroupsWithoutRefetching(t *testing.T) {
	organic, tracksByAlbum := relatedSearchFixture()
	querier := &countingRelationshipQuerier{}
	albumProvider := &countingAlbumTracksProvider{tracksByAlbum: tracksByAlbum}
	svc := NewService(
		[]ports.SearchProvider{&fakeProvider{name: domain.ProviderDeezer, results: organic}},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithFindRelatedService(NewFindRelatedService(querier, albumProvider, nil)),
	)
	user := newUser()

	first := searchAs(t, svc, user, "humble")
	providerCalls, libraryCalls := albumProvider.calls.Load(), querier.calls.Load()
	if providerCalls != relatedTopN || libraryCalls != relatedTopN {
		t.Fatalf("first search: provider calls = %d, library queries = %d, want %d each",
			providerCalls, libraryCalls, relatedTopN)
	}

	second := searchAs(t, svc, user, "humble")

	if got := albumProvider.calls.Load() - providerCalls; got != 0 {
		t.Errorf("second search: provider calls = %d, want 0", got)
	}
	if got := querier.calls.Load() - libraryCalls; got != 0 {
		t.Errorf("second search: library queries = %d, want 0", got)
	}
	if len(second.Related) != len(first.Related) {
		t.Errorf("second search: related groups = %d, want %d (same slate, same groups)",
			len(second.Related), len(first.Related))
	}
}

func TestService_RepeatedSearch_KeepsRelatedLibraryMatchesPerUser(t *testing.T) {
	organic, tracksByAlbum := relatedSearchFixture()
	owner, other := newUser(), newUser()
	querier := &countingRelationshipQuerier{
		matchesByUser: map[shared.UserId][]ports.RelatedTrackMatch{
			owner: {{Title: "Owner Library Track", Artist: "Artist 0", Album: "Album 0"}},
			other: {{Title: "Other Library Track", Artist: "Artist 0", Album: "Album 0"}},
		},
	}
	svc := NewService(
		[]ports.SearchProvider{&fakeProvider{name: domain.ProviderDeezer, results: organic}},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithFindRelatedService(NewFindRelatedService(querier, &countingAlbumTracksProvider{tracksByAlbum: tracksByAlbum}, nil)),
	)

	searchAs(t, svc, owner, "humble")
	otherOut := searchAs(t, svc, other, "humble")

	got := libraryTitles(otherOut)
	if len(got) == 0 {
		t.Fatal("second user got no library matches")
	}
	for _, title := range got {
		if title != "Other Library Track" {
			t.Errorf("second user was served %q, want only its own library matches", title)
		}
	}
}
