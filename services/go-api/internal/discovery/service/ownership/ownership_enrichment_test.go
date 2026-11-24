package ownership_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type stubOwnershipReader struct {
	owned map[string]discovery.OwnedTrack
	err   error
}

func (r stubOwnershipReader) OwnedByTitleArtist(context.Context, shared.UserId) (map[string]discovery.OwnedTrack, error) {
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

func fillableItem() discovery.OwnableItem {
	item, _ := ownable("track", "Song", "Artist", map[string]any{
		"owned_track_id": "track-1",
	})
	item.Album, item.ISRC, item.TrackPosition = "Album", "USRC17607839", discovery.TrackPosition{Disc: 1, Track: 3}
	return item
}

func fillableReader() stubOwnershipReader {
	return stubOwnershipReader{owned: map[string]discovery.OwnedTrack{
		discovery.OwnershipKey("Song", "Artist"): {TrackID: "track-1", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"},
	}}
}

func ownable(kind, title, artist string, extras map[string]any) (discovery.OwnableItem, *map[string]any) {
	holder := &extras
	return discovery.OwnableItem{Kind: kind, Title: title, Artist: artist, Extras: holder}, holder
}

func TestStampOwnership_StampsOwnedTracksOnly(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	svc := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{owned: map[string]discovery.OwnedTrack{
		discovery.OwnershipKey("Song", "Artist"): {TrackID: "t1", AcquisitionStatus: "ready"},
	}}, nil)
	track, trackExtras := ownable("track", "Song", "Artist", nil)
	album, albumExtras := ownable("album", "Song", "Artist", map[string]any{})
	other, otherExtras := ownable("track", "Other", "Artist", map[string]any{})

	svc.StampOwnership(context.Background(), shared.UserId{}, []discovery.OwnableItem{track, album, other})

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
	bg := discoverytest.NewTracker(t)
	svc := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{owned: map[string]discovery.OwnedTrack{
		discovery.OwnershipKey("Song (Live)", "X"):  {TrackID: "live"},
		discovery.OwnershipKey("Song (Remix)", "X"): {TrackID: "remix"},
	}}, nil)
	studio, studioExtras := ownable("track", "Song", "X", map[string]any{})
	live, liveExtras := ownable("track", "Song (Live)", "X", map[string]any{})

	svc.StampOwnership(context.Background(), shared.UserId{}, []discovery.OwnableItem{studio, live})

	if len(*studioExtras) != 0 {
		t.Errorf("studio extras = %v, want unstamped", *studioExtras)
	}
	if got := (*liveExtras)["owned_track_id"]; got != "live" {
		t.Errorf("live owned_track_id = %v, want live", got)
	}
}

func TestStampOwnership_MatchesByISRCAndRefusesAConflictingISRC(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	svc := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{owned: map[string]discovery.OwnedTrack{
		discovery.OwnershipISRCKey("USRC17607839"):    {TrackID: "by-isrc", ISRC: "USRC17607839"},
		discovery.OwnershipKey("Song", "X"):           {TrackID: "other-recording", ISRC: "GBAAA0000001"},
		discovery.OwnershipKey("Respelled Song", "X"): {TrackID: "by-name"},
	}}, nil)
	sameRecording, sameExtras := ownable("track", "Song Retitled", "Y", map[string]any{})
	sameRecording.ISRC = "us-rc1-76-07839"
	otherRecording, otherExtras := ownable("track", "Song", "X", map[string]any{})
	otherRecording.ISRC = "USRC17607839x"
	unlabelled, unlabelledExtras := ownable("track", "Respelled Song", "X", map[string]any{})
	unlabelled.ISRC = "FRZZZ0000009"

	svc.StampOwnership(context.Background(), shared.UserId{}, []discovery.OwnableItem{sameRecording, otherRecording, unlabelled})

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
	bg := discoverytest.NewTracker(t)
	failing := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{err: errors.New("db down")}, nil)
	for name, svc := range map[string]*discovery.OwnershipEnrichmentService{"lookup failure": failing, "nil service": nil} {
		item, extras := ownable("track", "Song", "Artist", nil)
		svc.StampOwnership(context.Background(), shared.UserId{}, []discovery.OwnableItem{item})
		if *extras != nil {
			t.Errorf("%s: extras = %v, want untouched", name, *extras)
		}
	}
}

func TestEnrichAlbumTracks_PanickingFillerIsContained(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := discovery.NewOwnershipEnrichmentService(bg, fillableReader(), filler)
	item := fillableItem()

	done := svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []discovery.OwnableItem{item})

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

func TestEnrichAlbumTracks_StalledFillIsAbandonedAtItsTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bg := discoverytest.NewTracker(t)
		filler := stallingFiller()
		svc := discovery.NewOwnershipEnrichmentService(bg, fillableReader(), filler)
		start := time.Now()

		<-svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []discovery.OwnableItem{fillableItem()})

		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Errorf("stalled fill returned after %v, want it abandoned at its 10s deadline", elapsed)
		}
	})
}

func TestEnrichAlbumTracks_TheSharedTrackerWaitsUntilTheFillFinishes(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	filler := stallingFiller()
	svc := discovery.NewOwnershipEnrichmentService(bg, fillableReader(), filler)
	item := fillableItem()
	svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []discovery.OwnableItem{item})
	<-filler.entered

	drained := make(chan struct{})
	go func() { bg.Wait(); close(drained) }()

	select {
	case <-drained:
		t.Fatal("the tracker stopped waiting while the backfill was still writing")
	case <-time.After(50 * time.Millisecond):
	}
	close(filler.release)
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("the tracker never stopped waiting after the backfill finished")
	}
}

func TestEnrichAlbumTracks_AFillTheTrackerRefusesCompletesWithoutWriting(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	if !bg.Drain(context.Background()) {
		t.Fatal("an idle tracker did not drain")
	}
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := discovery.NewOwnershipEnrichmentService(bg, fillableReader(), filler)

	select {
	case <-svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []discovery.OwnableItem{fillableItem()}):
	case <-time.After(2 * time.Second):
		t.Fatal("a refused fill left its done channel open")
	}
	select {
	case <-filler.called:
		t.Fatal("a refused fill still wrote a track number")
	default:
	}
}

func TestEnrichAlbumTracks_NothingToFillCompletesImmediately(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "track-1"})
	item.TrackPosition = discovery.TrackPosition{Disc: 1, Track: 3}

	select {
	case <-svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, []discovery.OwnableItem{item}):
	default:
		t.Fatal("expected completion handle to be closed when nothing is pending")
	}
}

type fillWrite struct {
	trackId     string
	trackNumber int
}

type recordingTrackNumberFiller struct{ writes chan fillWrite }

func (f recordingTrackNumberFiller) FillTrackNumber(_ context.Context, _ shared.UserId, trackId string, trackNumber int) error {
	f.writes <- fillWrite{trackId: trackId, trackNumber: trackNumber}
	return nil
}

func enrichAndCollectWrites(t *testing.T, owned discovery.OwnedTrack, items ...discovery.OwnableItem) []fillWrite {
	t.Helper()
	bg := discoverytest.NewTracker(t)
	filler := recordingTrackNumberFiller{writes: make(chan fillWrite, 32)}
	svc := discovery.NewOwnershipEnrichmentService(bg, stubOwnershipReader{owned: map[string]discovery.OwnedTrack{
		discovery.OwnershipKey(owned.Title, owned.Artist): owned,
	}}, filler)
	select {
	case <-svc.EnrichAlbumTracks(context.Background(), shared.UserId{}, items):
	case <-time.After(2 * time.Second):
		t.Fatal("track number fill never completed")
	}
	close(filler.writes)
	var got []fillWrite
	for w := range filler.writes {
		got = append(got, w)
	}
	return got
}

func positioned(album, title string, disc, position int, isrc string) discovery.OwnableItem {
	item, _ := ownable("track", title, "Artist", map[string]any{})
	item.Album, item.ISRC, item.TrackPosition = album, isrc, discovery.TrackPosition{Disc: disc, Track: position}
	return item
}

func TestEnrichAlbumTracks_TwoDiscAlbumWritesTheProvidersPositionNotTheListIndex(t *testing.T) {
	owned := discovery.OwnedTrack{TrackID: "t-disc2", Title: "Bonus One", Artist: "Artist", ISRC: "USRC17607839", Album: "Deluxe"}

	got := enrichAndCollectWrites(t, owned,
		positioned("Deluxe", "Song A", 1, 1, "USRC10000001"),
		positioned("Deluxe", "Song B", 1, 2, "USRC10000002"),
		positioned("Deluxe", "Bonus One", 2, 1, "USRC17607839"),
	)

	if len(got) != 1 || got[0] != (fillWrite{trackId: "t-disc2", trackNumber: 1}) {
		t.Errorf("writes = %v, want only t-disc2 = 1", got)
	}
}

func TestEnrichAlbumTracks_CompilationPageWritesNothingOntoAStudioAlbumTrack(t *testing.T) {
	owned := discovery.OwnedTrack{TrackID: "t-studio", Title: "Hit", Artist: "Artist", ISRC: "USRC17607839", Album: "Studio Album"}

	got := enrichAndCollectWrites(t, owned, positioned("Greatest Hits", "Hit", 1, 9, "USRC17607839"))

	if len(got) != 0 {
		t.Errorf("writes = %v, want none", got)
	}
}

func TestEnrichAlbumTracks_NoWriteWithoutAnISRCMatchOrProviderPosition(t *testing.T) {
	owned := discovery.OwnedTrack{TrackID: "t1", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"}
	cases := map[string]discovery.OwnableItem{
		"other isrc":       positioned("Album", "Song", 1, 3, "GBAYE0000001"),
		"no isrc":          positioned("Album", "Song", 1, 3, ""),
		"no position":      positioned("Album", "Song", 0, 0, "USRC17607839"),
		"page album empty": positioned("", "Song", 1, 3, "USRC17607839"),
	}
	for name, item := range cases {
		if got := enrichAndCollectWrites(t, owned, item); len(got) != 0 {
			t.Errorf("%s: writes = %v, want none", name, got)
		}
	}
	albumless := discovery.OwnedTrack{TrackID: "t2", Title: "Song", Artist: "Artist", ISRC: "USRC17607839"}
	for name, pageAlbum := range map[string]string{"owned album empty, page album empty": "", "owned album empty, page album named": "Album"} {
		if got := enrichAndCollectWrites(t, albumless, positioned(pageAlbum, "Song", 1, 3, "USRC17607839")); len(got) != 0 {
			t.Errorf("%s: writes = %v, want none", name, got)
		}
	}
}

func typedItem(album, title string, position discovery.TrackPosition, isrc string) discovery.OwnableItem {
	item, _ := ownable("track", title, "Artist", map[string]any{})
	item.Album, item.ISRC, item.TrackPosition = album, isrc, position
	return item
}

func TestEnrichAlbumTracks_WritesTheTypedTrackPositionOfTheISRCMatch(t *testing.T) {
	owned := discovery.OwnedTrack{TrackID: "t-disc2", Title: "Bonus One", Artist: "Artist", ISRC: "USRC17607839", Album: "Deluxe"}

	got := enrichAndCollectWrites(t, owned,
		typedItem("Deluxe", "Song A", discovery.TrackPosition{Disc: 1, Track: 1}, "USRC10000001"),
		typedItem("Deluxe", "Bonus One", discovery.TrackPosition{Disc: 2, Track: 4}, "usrc-1760-7839"),
	)

	if len(got) != 1 || got[0] != (fillWrite{trackId: "t-disc2", trackNumber: 4}) {
		t.Errorf("writes = %v, want only t-disc2 = 4", got)
	}
}

func TestEnrichAlbumTracks_NoTypedWriteWithoutAPositionOrAnISRCMatch(t *testing.T) {
	owned := discovery.OwnedTrack{TrackID: "t1", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"}
	cases := map[string]discovery.OwnableItem{
		"unknown position": typedItem("Album", "Song", discovery.TrackPosition{}, "USRC17607839"),
		"other isrc":       typedItem("Album", "Song", discovery.TrackPosition{Disc: 1, Track: 3}, "GBAYE0000001"),
		"no isrc":          typedItem("Album", "Song", discovery.TrackPosition{Disc: 1, Track: 3}, ""),
	}
	for name, item := range cases {
		if got := enrichAndCollectWrites(t, owned, item); len(got) != 0 {
			t.Errorf("%s: writes = %v, want none", name, got)
		}
	}
}
