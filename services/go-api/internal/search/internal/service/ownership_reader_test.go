package service_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeLister struct {
	tracks []search.OwnedTrack
	err    error
}

func (f *fakeLister) ListOwnedTracks(context.Context, ids.UserId) ([]search.OwnedTrack, error) {
	return f.tracks, f.err
}

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

func TestOwnedByTitleArtist_MatchesNormalizedTitleAndArtist(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "track-1", Title: "Bohemian Rhapsody", Artist: "Queen", AcquisitionStatus: "ready"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	match, ok := owned[domain.OwnershipKey("bohemian rhapsody (remastered)", "QUEEN")]
	if !ok {
		t.Fatalf("expected a match for a differently-cased, bracket-suffixed title; got keys %v", owned)
	}
	if match.TrackID != "track-1" || match.AcquisitionStatus != "ready" {
		t.Errorf("match = %+v, want track-1/ready", match)
	}
}

func TestOwnedByTitleArtist_FirstRefWinsOnDuplicateKey(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "first", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "ready"},
		{TrackID: "second", Title: "Alive", Artist: "Pearl Jam", AcquisitionStatus: "failed"},
	}})

	owned, _ := reader.OwnedByTitleArtist(context.Background(), testUser())

	if got := owned[domain.OwnershipKey("Alive", "Pearl Jam")].TrackID; got != "first" {
		t.Errorf("TrackID = %q, want %q", got, "first")
	}
}

func TestOwnedByTitleArtist_LiveLibraryTrackIsNotKeyedAsTheStudioSong(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "live", Title: "Song (Live)", Artist: "X"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := owned[domain.OwnershipKey("Song", "X")]; ok {
		t.Errorf("studio Song keyed as owned by a live-only library: %v", owned)
	}
	if owned[domain.OwnershipKey("Song (Live)", "X")].TrackID != "live" {
		t.Errorf("live track not keyed by its own title: %v", owned)
	}
}

func TestOwnedByTitleArtist_ISRCReachesTheTrackThatLostTheNameKey(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "first", Title: "Alive", Artist: "Pearl Jam", ISRC: "USSM19100001"},
		{TrackID: "second", Title: "Alive", Artist: "Pearl Jam", ISRC: "us-sm1-91-00002"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := owned[domain.OwnershipISRCKey("USSM19100002")]; got.TrackID != "second" || got.ISRC != "USSM19100002" {
		t.Errorf("ISRC lookup = %+v, want second carrying the canonical ISRC USSM19100002", got)
	}
	if got := owned[domain.OwnershipISRCKey("USSM19100001")].TrackID; got != "first" {
		t.Errorf("ISRC lookup TrackID = %q, want first", got)
	}
}

func TestOwnedByTitleArtist_PropagatesError(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{err: errors.New("db down")})

	if _, err := reader.OwnedByTitleArtist(context.Background(), testUser()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOwnedByTitleArtist_ZeroRefsGivesEmptyMap(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owned == nil || len(owned) != 0 {
		t.Errorf("owned = %v, want empty non-nil map", owned)
	}
}

func TestOwnedByTitleArtist_OneTrackKeepsEveryField(t *testing.T) {
	num := 4
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "t-1", Title: "Black", Artist: "Pearl Jam", AcquisitionStatus: "ready", TrackNumber: &num},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("len(owned) = %d, want 1", len(owned))
	}
	got := owned[domain.OwnershipKey("Black", "Pearl Jam")]
	if got.TrackID != "t-1" || got.Title != "Black" || got.Artist != "Pearl Jam" ||
		got.AcquisitionStatus != "ready" || got.TrackNumber != &num {
		t.Errorf("got %+v, want every field of the track unchanged", got)
	}
}

func TestOwnedByTitleArtist_SameTitleArtistDifferentCaseKeepsFirst(t *testing.T) {
	reader := search.NewOwnershipReader(&fakeLister{tracks: []search.OwnedTrack{
		{TrackID: "first", Title: "Alive", Artist: "Pearl Jam"},
		{TrackID: "second", Title: "ALIVE", Artist: "PEARL JAM"},
	}})

	owned, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("len(owned) = %d, want 1", len(owned))
	}
	if got := owned[domain.OwnershipKey("alive", "pearl jam")].TrackID; got != "first" {
		t.Errorf("TrackID = %q, want %q", got, "first")
	}
}

func TestOwnedByTitleArtist_WrapsListerError(t *testing.T) {
	cause := errors.New("db down")
	reader := search.NewOwnershipReader(&fakeLister{err: cause})

	_, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap %v", err, cause)
	}
	if !strings.Contains(err.Error(), "read owned tracks") {
		t.Errorf("err = %q, want it to contain %q", err, "read owned tracks")
	}
}

func TestOwnedByTitleArtist_SecondCallReturnsSecondMap(t *testing.T) {
	lister := &fakeLister{tracks: []search.OwnedTrack{{TrackID: "a", Title: "One", Artist: "X"}}}
	reader := search.NewOwnershipReader(lister)

	first, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lister.tracks = []search.OwnedTrack{{TrackID: "b", Title: "Two", Artist: "Y"}}
	second, err := reader.OwnedByTitleArtist(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first[domain.OwnershipKey("One", "X")].TrackID != "a" || len(first) != 1 {
		t.Errorf("first = %v, want only a", first)
	}
	if len(second) != 1 || second[domain.OwnershipKey("Two", "Y")].TrackID != "b" {
		t.Errorf("second = %v, want only b", second)
	}
}
