package catalogbridge

import (
	catalogdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeLister struct {
	tracks []catalogdomain.OwnedTrackRef
	err    error
}

func (f *fakeLister) ListOwnedTrackRefs(context.Context, shared.UserId) ([]catalogdomain.OwnedTrackRef, error) {
	return f.tracks, f.err
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestOwnedByTitleArtist_MatchesNormalizedTitleAndArtist(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{tracks: []catalogdomain.OwnedTrackRef{
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
	reader := NewOwnershipReader(&fakeLister{tracks: []catalogdomain.OwnedTrackRef{
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
	calls   int
	lastNum int
	lastId  string
}

func (r *recordingSetter) Execute(_ context.Context, _ shared.UserId, trackId catalogdomain.TrackId, n int) (bool, error) {
	r.calls++
	r.lastNum = n
	r.lastId = trackId.String()
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

func TestOwnedByTitleArtist_ZeroRefsGivesEmptyMap(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owned == nil || len(owned) != 0 {
		t.Errorf("owned = %v, want empty non-nil map", owned)
	}
}

func TestOwnedByTitleArtist_OneRefMapsEveryField(t *testing.T) {
	num := 4
	reader := NewOwnershipReader(&fakeLister{tracks: []catalogdomain.OwnedTrackRef{
		{ID: "t-1", Title: "Black", Artist: "Pearl Jam", AcquisitionStatus: "ready", TrackNumber: &num},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("len(owned) = %d, want 1", len(owned))
	}
	got := owned[ports.OwnershipKey("Black", "Pearl Jam")]
	if got.TrackID != "t-1" || got.Title != "Black" || got.Artist != "Pearl Jam" ||
		got.AcquisitionStatus != "ready" || got.TrackNumber != &num {
		t.Errorf("got %+v, want all fields of the ref unchanged", got)
	}
}

func TestOwnedByTitleArtist_SameTitleArtistDifferentCaseKeepsFirst(t *testing.T) {
	reader := NewOwnershipReader(&fakeLister{tracks: []catalogdomain.OwnedTrackRef{
		{ID: "first", Title: "Alive", Artist: "Pearl Jam"},
		{ID: "second", Title: "ALIVE", Artist: "PEARL JAM"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("len(owned) = %d, want 1", len(owned))
	}
	if got := owned[ports.OwnershipKey("alive", "pearl jam")].TrackID; got != "first" {
		t.Errorf("TrackID = %q, want %q", got, "first")
	}
}

func TestOwnedByTitleArtist_WrapsListerError(t *testing.T) {
	cause := errors.New("db down")
	reader := NewOwnershipReader(&fakeLister{err: cause})

	_, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap %v", err, cause)
	}
	if !strings.Contains(err.Error(), "read owned tracks") {
		t.Errorf("err = %q, want it to contain %q", err, "read owned tracks")
	}
}

func TestOwnedByTitleArtist_SecondCallReturnsSecondMap(t *testing.T) {
	lister := &fakeLister{tracks: []catalogdomain.OwnedTrackRef{{ID: "a", Title: "One", Artist: "X"}}}
	reader := NewOwnershipReader(lister)

	first, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lister.tracks = []catalogdomain.OwnedTrackRef{{ID: "b", Title: "Two", Artist: "Y"}}
	second, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first[ports.OwnershipKey("One", "X")].TrackID != "a" || len(first) != 1 {
		t.Errorf("first = %v, want only a", first)
	}
	if len(second) != 1 || second[ports.OwnershipKey("Two", "Y")].TrackID != "b" {
		t.Errorf("second = %v, want only b", second)
	}
}

func TestFillTrackNumber_MalformedIdRefusedWithoutCallingSetter(t *testing.T) {
	setter := &recordingSetter{}
	writer := NewTrackNumberWriter(setter)

	err := writer.FillTrackNumber(context.Background(), testUser(), "not-a-uuid", 3)

	if err == nil || !strings.Contains(err.Error(), "parse track id") {
		t.Fatalf("err = %v, want one containing %q", err, "parse track id")
	}
	if setter.calls != 0 {
		t.Errorf("setter called %d times, want 0", setter.calls)
	}
}
