package catalogbridge

import (
	"context"
	"errors"
	"testing"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type fakeLister struct {
	refs []catalogDomain.OwnedTrackRef
	err  error
}

func (f *fakeLister) ListOwnedTrackRefs(context.Context, shared.UserId) ([]catalogDomain.OwnedTrackRef, error) {
	return f.refs, f.err
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestOwnedByTitleArtist_MatchesNormalizedTitleAndArtist(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{refs: []catalogDomain.OwnedTrackRef{
		{ID: "track-1", Title: "Bohemian Rhapsody", Artist: "Queen", AcquisitionStatus: "ready"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	match, ok := owned[ports.OwnershipKey("bohemian rhapsody (remastered)", "QUEEN")]
	if !ok {
		t.Fatalf("expected a match for a differently-cased, bracket-suffixed title; got keys %v", owned)
	}
	if match.TrackID != "track-1" || match.AcquisitionStatus != "ready" {
		t.Errorf("match = %+v, want track-1/ready", match)
	}
}

func TestOwnedByTitleArtist_FirstRefWinsOnDuplicateKey(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{refs: []catalogDomain.OwnedTrackRef{
		{ID: "first", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "ready"},
		{ID: "second", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "failed"},
	}})

	owned, _ := reader.OwnedByTitleArtist(context.Background(), testUser())

	if got := owned[ports.OwnershipKey("Alive", "Pearl Jam")].TrackID; got != "first" {
		t.Errorf("TrackID = %q, want %q", got, "first")
	}
}

func TestOwnedByTitleArtist_PropagatesError(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{err: errors.New("db down")})

	if _, err := reader.OwnedByTitleArtist(context.Background(), testUser()); err == nil {
		t.Fatal("expected an error")
	}
}

type recordingSetter struct {
	calls int
	last  int
}

func (r *recordingSetter) Execute(_ context.Context, _ shared.UserId, _ catalogDomain.TrackId, n int) (bool, error) {
	r.calls++
	r.last = n
	return true, nil
}

func TestFillTrackNumber_IgnoresMalformedId(t *testing.T) {
	setter := &recordingSetter{}
	writer := NewTrackNumberWriter(setter)

	if err := writer.FillTrackNumber(context.Background(), testUser(), "not-a-uuid", 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setter.calls != 0 {
		t.Errorf("setter called %d times, want 0", setter.calls)
	}
}

func TestFillTrackNumber_WritesPosition(t *testing.T) {
	setter := &recordingSetter{}
	writer := NewTrackNumberWriter(setter)

	if err := writer.FillTrackNumber(context.Background(), testUser(), uuid.New().String(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setter.calls != 1 || setter.last != 7 {
		t.Errorf("setter calls=%d last=%d, want 1/7", setter.calls, setter.last)
	}
}
