package discoverycatalog_test

import (
	"altune/go-api/internal/app/discoverycatalog"
	catalogdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeRefs struct {
	refs []catalogdomain.OwnedTrackRef
	err  error
}

func (f fakeRefs) ListOwnedTrackRefs(context.Context, shared.UserId) ([]catalogdomain.OwnedTrackRef, error) {
	return f.refs, f.err
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestListOwnedTracks_CarriesEveryFieldOfEachRefInOrder(t *testing.T) {
	num := 4
	tracks, err := discoverycatalog.NewOwnedTracks(fakeRefs{refs: []catalogdomain.OwnedTrackRef{
		{ID: "t-1", Title: "Black", Artist: "Pearl Jam", AcquisitionStatus: "ready", TrackNumber: &num, ISRC: "us-sm1-91-00002", Album: "Ten"},
		{ID: "t-2", Title: "Alive", Artist: "Pearl Jam"},
	}}).ListOwnedTracks(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []discovery.OwnedTrack{
		{TrackID: "t-1", Title: "Black", Artist: "Pearl Jam", AcquisitionStatus: "ready", TrackNumber: &num, ISRC: "us-sm1-91-00002", Album: "Ten"},
		{TrackID: "t-2", Title: "Alive", Artist: "Pearl Jam"},
	}
	if !reflect.DeepEqual(tracks, want) || tracks[0].TrackNumber != &num {
		t.Errorf("tracks = %+v, want %+v", tracks, want)
	}
}

func TestListOwnedTracks_NoRefsIsNoTracks(t *testing.T) {
	tracks, err := discoverycatalog.NewOwnedTracks(fakeRefs{}).ListOwnedTracks(context.Background(), testUser())

	if err != nil || len(tracks) != 0 {
		t.Errorf("tracks = %v, err = %v; want none and no error", tracks, err)
	}
}

func TestListOwnedTracks_ReturnsTheCatalogError(t *testing.T) {
	cause := errors.New("db down")

	tracks, err := discoverycatalog.NewOwnedTracks(fakeRefs{err: cause}).ListOwnedTracks(context.Background(), testUser())

	if !errors.Is(err, cause) || tracks != nil {
		t.Errorf("tracks = %v, err = %v; want none and %v", tracks, err, cause)
	}
}

type recordingSetter struct {
	calls   int
	lastNum int
	lastId  string
	err     error
}

func (r *recordingSetter) Execute(_ context.Context, _ shared.UserId, trackId catalogdomain.TrackId, n int) (bool, error) {
	r.calls++
	r.lastNum = n
	r.lastId = trackId.String()
	return r.err == nil, r.err
}

func TestFillTrackNumber_PassesIdAndPositionThrough(t *testing.T) {
	setter := &recordingSetter{}
	filler := discoverycatalog.NewTrackNumberFiller(setter)

	id := uuid.New().String()
	if err := filler.FillTrackNumber(context.Background(), testUser(), id, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setter.calls != 1 || setter.lastNum != 7 || setter.lastId != id {
		t.Errorf("setter calls=%d lastNum=%d lastId=%q, want 1/7/%q", setter.calls, setter.lastNum, setter.lastId, id)
	}
}

func TestFillTrackNumber_MalformedIdRefusedWithoutCallingSetter(t *testing.T) {
	setter := &recordingSetter{}
	filler := discoverycatalog.NewTrackNumberFiller(setter)

	err := filler.FillTrackNumber(context.Background(), testUser(), "not-a-uuid", 3)

	if err == nil || !strings.Contains(err.Error(), "parse track id") {
		t.Fatalf("err = %v, want one containing %q", err, "parse track id")
	}
	if setter.calls != 0 {
		t.Errorf("setter called %d times, want 0", setter.calls)
	}
}

func TestFillTrackNumber_WrapsTheSettersError(t *testing.T) {
	cause := errors.New("db down")
	filler := discoverycatalog.NewTrackNumberFiller(&recordingSetter{err: cause})

	err := filler.FillTrackNumber(context.Background(), testUser(), uuid.New().String(), 3)

	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "fill track number") {
		t.Fatalf("err = %v, want it to wrap %v under %q", err, cause, "fill track number")
	}
}
