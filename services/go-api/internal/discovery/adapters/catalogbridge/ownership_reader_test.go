package catalogbridge

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeLister struct {
	tracks []ports.OwnedTrack
	err    error
}

func (f *fakeLister) ListOwnedTracks(context.Context, shared.UserId) ([]ports.OwnedTrack, error) {
	return f.tracks, f.err
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestOwnedByTitleArtist_MatchesNormalizedTitleAndArtist(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{tracks: []ports.OwnedTrack{
		{TrackID: "track-1", Title: "Bohemian Rhapsody", Artist: "Queen", AcquisitionStatus: "ready"},
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
	reader := NewOwnershipReader(&fakeLister{tracks: []ports.OwnedTrack{
		{TrackID: "first", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "ready"},
		{TrackID: "second", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "failed"},
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
	calls   int
	lastNum int
	lastId  string
}

func (r *recordingSetter) Execute(_ context.Context, _ shared.UserId, trackId string, n int) (bool, error) {
	r.calls++
	r.lastNum = n
	r.lastId = trackId
	return true, nil
}

func TestFillTrackNumber_PassesIdAndPositionThrough(t *testing.T) {
	setter := &recordingSetter{}
	writer := NewTrackNumberWriter(setter)

	id := uuid.New().String()
	if err := writer.FillTrackNumber(context.Background(), testUser(), id, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setter.calls != 1 || setter.lastNum != 7 || setter.lastId != id {
		t.Errorf("setter calls=%d lastNum=%d lastId=%q, want 1/7/%q", setter.calls, setter.lastNum, setter.lastId, id)
	}
}
