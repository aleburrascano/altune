package main

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRepo struct {
	track    *domain.Track
	updated  []*domain.Track
	updraise error
}

func (r *fakeRepo) GetByID(context.Context, domain.TrackId, shared.UserId) (*domain.Track, error) {
	return r.track, nil
}

func (r *fakeRepo) Update(_ context.Context, track *domain.Track) error {
	if r.updraise != nil {
		return r.updraise
	}
	r.updated = append(r.updated, track)
	return nil
}

func testCandidate() candidate {
	return candidate{
		trackID: domain.NewTrackId(),
		userID:  shared.NewUserId(uuid.New()),
		title:   "Crook",
		artist:  "Raf Saperra",
	}
}

func albumlessTrack(t *testing.T, c candidate) *domain.Track {
	t.Helper()
	return &domain.Track{
		ID:     c.trackID,
		UserId: c.userID,
		Title:  c.title,
		Artist: c.artist,
		Album:  "",
	}
}

func TestRetitle_SetsTheAlbumToTheTitleOnApply(t *testing.T) {
	c := testCandidate()
	repo := &fakeRepo{track: albumlessTrack(t, c)}

	if err := retitle(context.Background(), repo, []candidate{c}, true); err != nil {
		t.Fatalf("retitle: %v", err)
	}

	if len(repo.updated) != 1 {
		t.Fatalf("persisted %d tracks, want 1", len(repo.updated))
	}
	if repo.updated[0].Album != "Crook" {
		t.Errorf("Album = %q, want the title", repo.updated[0].Album)
	}
	if repo.updated[0].DedupKey == "" {
		t.Error("DedupKey was not recomputed")
	}
}

func TestRetitle_DryRunNeverWrites(t *testing.T) {
	c := testCandidate()
	repo := &fakeRepo{track: albumlessTrack(t, c)}

	if err := retitle(context.Background(), repo, []candidate{c}, false); err != nil {
		t.Fatalf("retitle: %v", err)
	}

	if len(repo.updated) != 0 {
		t.Error("dry run persisted a track")
	}
}

func TestRetitle_SkipsADuplicateInsteadOfFailingTheRun(t *testing.T) {
	c := testCandidate()
	repo := &fakeRepo{
		track:    albumlessTrack(t, c),
		updraise: &pgconn.PgError{Code: uniqueViolation},
	}

	if err := retitle(context.Background(), repo, []candidate{c}, true); err != nil {
		t.Fatalf("a dedup collision must not abort the run: %v", err)
	}
}

func TestRetitle_StopsOnAnUnexpectedError(t *testing.T) {
	c := testCandidate()
	repo := &fakeRepo{track: albumlessTrack(t, c), updraise: errors.New("connection lost")}

	if err := retitle(context.Background(), repo, []candidate{c}, true); err == nil {
		t.Fatal("expected an unexpected write failure to abort the run")
	}
}
