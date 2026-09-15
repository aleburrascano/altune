package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"

	discoveryDomain "altune/go-api/internal/discovery/domain"

	"github.com/google/uuid"
)

func TestIsBlankOrSuspect(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"empty art hash", "https://cdn/" + emptyArtHash + ".jpg", true},
		{"deezer artist placeholder", "https://e-cdns-images.dzcdn.net/images/artist//500x500.jpg", true},
		{"real cover", "https://cdn/covers/abc.jpg", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBlankOrSuspect(tc.url); got != tc.want {
				t.Fatalf("isBlankOrSuspect(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

type fakeResolver struct {
	byTitle map[string]string
}

func (f fakeResolver) ResolveTagged(_ context.Context, _ discoveryDomain.ResultKind, title, _, _ string) (string, discoveryDomain.ProviderKey, error) {
	return f.byTitle[title], "fake", nil
}

type fakeRepo struct {
	tracks  map[string]*domain.Track
	updated map[string]string // trackID -> new artwork url
}

func newFakeRepo(candidates []candidate) *fakeRepo {
	r := &fakeRepo{tracks: map[string]*domain.Track{}, updated: map[string]string{}}
	for _, c := range candidates {
		track, err := domain.NewTrack(c.userID, c.title, c.artist, "")
		if err != nil {
			panic(err)
		}
		track.ID = c.trackID
		r.tracks[c.trackID.String()] = track
	}
	return r
}

func (r *fakeRepo) GetByID(_ context.Context, id domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	return r.tracks[id.String()], nil
}

func (r *fakeRepo) Update(_ context.Context, track *domain.Track) error {
	url := ""
	if track.ArtworkURL != nil {
		url = *track.ArtworkURL
	}
	r.updated[track.ID.String()] = url
	return nil
}

func mustCandidate(t *testing.T, title, artist, old string) candidate {
	t.Helper()
	return candidate{
		trackID:    domain.NewTrackId(),
		userID:     shared.NewUserId(uuid.New()),
		title:      title,
		artist:     artist,
		oldArtwork: old,
	}
}

func TestHealDryRunWritesNothing(t *testing.T) {
	cands := []candidate{mustCandidate(t, "Song A", "Artist A", "")}
	repo := newFakeRepo(cands)
	resolver := fakeResolver{byTitle: map[string]string{"Song A": "https://cdn/new-a.jpg"}}

	if err := heal(context.Background(), repo, resolver, cands, false); err != nil {
		t.Fatalf("heal dry run: %v", err)
	}
	if len(repo.updated) != 0 {
		t.Fatalf("dry run wrote %d rows, want 0", len(repo.updated))
	}
}

func TestHealApplyUpdatesResolvedCovers(t *testing.T) {
	blank := mustCandidate(t, "Song A", "Artist A", "")
	unresolvable := mustCandidate(t, "Song B", "Artist B", "")
	sameCover := mustCandidate(t, "Song C", "Artist C", "https://cdn/same-c.jpg")
	suspectResult := mustCandidate(t, "Song D", "Artist D", "")
	cands := []candidate{blank, unresolvable, sameCover, suspectResult}

	repo := newFakeRepo(cands)
	resolver := fakeResolver{byTitle: map[string]string{
		"Song A": "https://cdn/new-a.jpg",
		"Song B": "", // resolver finds nothing
		"Song C": "https://cdn/same-c.jpg",
		"Song D": "https://e-cdns-images.dzcdn.net/images/artist//x.jpg", // still a placeholder
	}}

	if err := heal(context.Background(), repo, resolver, cands, true); err != nil {
		t.Fatalf("heal apply: %v", err)
	}

	if got := repo.updated[blank.trackID.String()]; got != "https://cdn/new-a.jpg" {
		t.Fatalf("blank cover = %q, want new-a", got)
	}
	if _, ok := repo.updated[unresolvable.trackID.String()]; ok {
		t.Fatalf("unresolvable track should not be updated")
	}
	if _, ok := repo.updated[sameCover.trackID.String()]; ok {
		t.Fatalf("track whose resolved cover equals stored cover should not be updated")
	}
	if _, ok := repo.updated[suspectResult.trackID.String()]; ok {
		t.Fatalf("track whose resolved cover is still a placeholder should not be updated")
	}
	if len(repo.updated) != 1 {
		t.Fatalf("updated %d rows, want 1", len(repo.updated))
	}
}
