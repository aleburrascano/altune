package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"testing"

	"github.com/google/uuid"
)

type ownedTrackRow struct {
	owner shared.UserId
	match ports.RelatedTrackMatch
}

// ownershipQuerier holds library rows for several users, like the shared
// tracks table, and answers related-track lookups for the caller.
type ownershipQuerier struct {
	rows []ownedTrackRow
}

func (q *ownershipQuerier) FindRelatedByAlbum(_ context.Context, userId shared.UserId, album string, _ int) ([]ports.RelatedTrackMatch, error) {
	var out []ports.RelatedTrackMatch
	for _, r := range q.rows {
		if r.owner == userId && r.match.Album == album {
			out = append(out, r.match)
		}
	}
	return out, nil
}

// Regression for #570: a search must never surface another user's private
// library tracks in its related groups.
func TestSearch_RelatedLibraryMatches_NeverLeakAnotherUsersLibrary(t *testing.T) {
	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())
	const album = "Shared Album"

	querier := &ownershipQuerier{rows: []ownedTrackRow{
		{owner: userA, match: ports.RelatedTrackMatch{Title: "A Private Song", Artist: "Band", Album: album}},
		{owner: userB, match: ports.RelatedTrackMatch{Title: "B Own Song", Artist: "Band", Album: album}},
	}}

	organic := deezerTrack("Hit Single", "Band", 80)
	organic.Album = album
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{organic}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(),
		WithFindRelatedService(NewFindRelatedService(querier, nil, nil)))

	out, err := svc.Execute(context.Background(), userB, newQuery(t, "hit single"), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sawOwn bool
	for _, g := range out.Related {
		for _, item := range g.Items {
			if item.Extras["source"] != "library" {
				continue
			}
			if item.Title == "A Private Song" {
				t.Errorf("user B's search leaked user A's library track %q in group %q", item.Title, g.Relationship)
			}
			if item.Title == "B Own Song" {
				sawOwn = true
			}
		}
	}
	if !sawOwn {
		t.Error("user B's own library track should still surface as a library match")
	}
}
