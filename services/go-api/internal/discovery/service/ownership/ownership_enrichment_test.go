package ownership

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"
)

type stubOwnershipReader struct {
	owned map[string]ports.OwnedTrack
	err   error
}

func (r stubOwnershipReader) OwnedByTitleArtist(context.Context, shared.UserId) (map[string]ports.OwnedTrack, error) {
	return r.owned, r.err
}

type panickingTrackNumberFiller struct{ called chan struct{} }

func (f panickingTrackNumberFiller) FillTrackNumber(context.Context, shared.UserId, string, int) error {
	defer close(f.called)
	panic("track number fill exploded")
}

type stallingTrackNumberFiller struct {
	entered chan struct{}
	release chan struct{}
}

func (f stallingTrackNumberFiller) FillTrackNumber(ctx context.Context, _ shared.UserId, _ string, _ int) error {
	close(f.entered)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.release:
		return nil
	}
}

func stallingFiller() stallingTrackNumberFiller {
	return stallingTrackNumberFiller{entered: make(chan struct{}), release: make(chan struct{})}
}

func ownable(kind, title, artist string, extras map[string]any) (OwnableItem, *map[string]any) {
	holder := &extras
	return OwnableItem{Kind: kind, Title: title, Artist: artist, Extras: holder}, holder
}

func TestStampOwnership_StampsOwnedTracksOnly(t *testing.T) {
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{owned: map[string]ports.OwnedTrack{
		domain.OwnershipKey("Song", "Artist"): {TrackID: "t1", AcquisitionStatus: "ready"},
	}}, nil)
	track, trackExtras := ownable("track", "Song", "Artist", nil)
	album, albumExtras := ownable("album", "Song", "Artist", map[string]any{})
	other, otherExtras := ownable("track", "Other", "Artist", map[string]any{})

	svc.StampOwnership(context.Background(), shared.UserId{}, []OwnableItem{track, album, other})

	if got := (*trackExtras)["owned_track_id"]; got != "t1" {
		t.Errorf("track owned_track_id = %v, want t1", got)
	}
	if got := (*trackExtras)["owned_acquisition_status"]; got != "ready" {
		t.Errorf("track owned_acquisition_status = %v, want ready", got)
	}
	if len(*albumExtras) != 0 || len(*otherExtras) != 0 {
		t.Errorf("album %v / unowned track %v must stay unstamped", *albumExtras, *otherExtras)
	}
}

func TestStampOwnership_VersionedLibraryTrackDoesNotOwnTheStudioSong(t *testing.T) {
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{owned: map[string]ports.OwnedTrack{
		domain.OwnershipKey("Song (Live)", "X"):  {TrackID: "live"},
		domain.OwnershipKey("Song (Remix)", "X"): {TrackID: "remix"},
	}}, nil)
	studio, studioExtras := ownable("track", "Song", "X", map[string]any{})
	live, liveExtras := ownable("track", "Song (Live)", "X", map[string]any{})

	svc.StampOwnership(context.Background(), shared.UserId{}, []OwnableItem{studio, live})

	if len(*studioExtras) != 0 {
		t.Errorf("studio extras = %v, want unstamped", *studioExtras)
	}
	if got := (*liveExtras)["owned_track_id"]; got != "live" {
		t.Errorf("live owned_track_id = %v, want live", got)
	}
}

func TestStampOwnership_MatchesByISRCAndRefusesAConflictingISRC(t *testing.T) {
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{owned: map[string]ports.OwnedTrack{
		domain.OwnershipISRCKey("USRC17607839"):    {TrackID: "by-isrc", ISRC: "USRC17607839"},
		domain.OwnershipKey("Song", "X"):           {TrackID: "other-recording", ISRC: "GBAAA0000001"},
		domain.OwnershipKey("Respelled Song", "X"): {TrackID: "by-name"},
	}}, nil)
	sameRecording, sameExtras := ownable("track", "Song Retitled", "Y", map[string]any{"isrc": "us-rc1-76-07839"})
	otherRecording, otherExtras := ownable("track", "Song", "X", map[string]any{"isrc": "USRC17607839x"})
	unlabelled, unlabelledExtras := ownable("track", "Respelled Song", "X", map[string]any{"isrc": "FRZZZ0000009"})

	svc.StampOwnership(context.Background(), shared.UserId{}, []OwnableItem{sameRecording, otherRecording, unlabelled})

	if got := (*sameExtras)["owned_track_id"]; got != "by-isrc" {
		t.Errorf("same recording owned_track_id = %v, want by-isrc", got)
	}
	if got, stamped := (*otherExtras)["owned_track_id"]; stamped {
		t.Errorf("conflicting ISRC owned_track_id = %v, want unstamped", got)
	}
	if got := (*unlabelledExtras)["owned_track_id"]; got != "by-name" {
		t.Errorf("owned track without ISRC owned_track_id = %v, want by-name", got)
	}
}

func TestStampOwnership_LookupFailureAndNilServiceLeaveItemsUntouched(t *testing.T) {
	failing := NewOwnershipEnrichmentService(stubOwnershipReader{err: errors.New("db down")}, nil)
	for name, svc := range map[string]*OwnershipEnrichmentService{"lookup failure": failing, "nil service": nil} {
		item, extras := ownable("track", "Song", "Artist", nil)
		svc.StampOwnership(context.Background(), shared.UserId{}, []OwnableItem{item})
		if *extras != nil {
			t.Errorf("%s: extras = %v, want untouched", name, *extras)
		}
	}
}

func TestEnrichAlbumTracks_PanickingFillerIsContained(t *testing.T) {
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "track-1"})

	done := svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []OwnableItem{item})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("track number fill never completed")
	}
	select {
	case <-filler.called:
	default:
		t.Fatal("filler was never called")
	}
}

func TestEnrichAlbumTracks_StalledFillIsAbandonedAtItsDeadline(t *testing.T) {
	restore := trackNumberFillTimeout
	trackNumberFillTimeout = 20 * time.Millisecond
	t.Cleanup(func() { trackNumberFillTimeout = restore })
	filler := stallingFiller()
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "track-1"})

	done := svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []OwnableItem{item})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fill against a stalled writer never returned: the backfill has no deadline")
	}
}

func TestEnrichAlbumTracks_WaitForBackgroundBlocksUntilTheFillFinishes(t *testing.T) {
	filler := stallingFiller()
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "track-1"})
	svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []OwnableItem{item})
	<-filler.entered

	drained := make(chan struct{})
	go func() { svc.WaitForBackground(); close(drained) }()

	select {
	case <-drained:
		t.Fatal("WaitForBackground returned while the backfill was still writing")
	case <-time.After(50 * time.Millisecond):
	}
	close(filler.release)
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForBackground never returned after the backfill finished")
	}
}

func TestEnrichAlbumTracks_NothingToFillCompletesImmediately(t *testing.T) {
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "track-1", "track_position": 3})

	select {
	case <-svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []OwnableItem{item}):
	default:
		t.Fatal("expected completion handle to be closed when nothing is pending")
	}
}
