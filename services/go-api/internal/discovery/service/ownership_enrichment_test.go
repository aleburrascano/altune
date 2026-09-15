package service

import (
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

func ownable(kind, title, artist string, extras map[string]any) (OwnableItem, *map[string]any) {
	holder := &extras
	return OwnableItem{Kind: kind, Title: title, Artist: artist, Extras: holder}, holder
}

func TestStampOwnership_StampsOwnedTracksOnly(t *testing.T) {
	svc := NewOwnershipEnrichmentService(stubOwnershipReader{owned: map[string]ports.OwnedTrack{
		ports.OwnershipKey("Song", "Artist"): {TrackID: "t1", AcquisitionStatus: "ready"},
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

	// done closes only once the detached goroutine has recovered the panic and
	// finished; an unrecovered panic crashes the binary before it closes.
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
