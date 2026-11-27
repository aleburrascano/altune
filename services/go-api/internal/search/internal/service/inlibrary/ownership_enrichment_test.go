package inlibrary_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/inlibrary"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type stubOwnershipReader struct {
	owned map[string]search.OwnedTrack
	err   error
}

func (r stubOwnershipReader) OwnedByTitleArtist(context.Context, ids.UserId) (map[string]search.OwnedTrack, error) {
	return r.owned, r.err
}

type panickingTrackNumberFiller struct{ called chan struct{} }

func (f panickingTrackNumberFiller) FillTrackNumber(context.Context, ids.UserId, ids.TrackId, int) error {
	defer close(f.called)
	panic("track number fill exploded")
}

type stallingTrackNumberFiller struct {
	entered chan struct{}
	release chan struct{}
}

func (f stallingTrackNumberFiller) FillTrackNumber(ctx context.Context, _ ids.UserId, _ ids.TrackId, _ int) error {
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

func fillableItem() inlibrary.OwnableItem {
	item, _ := ownable("track", "Song", "Artist", map[string]any{
		"owned_track_id": "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0001",
	})
	item.Album, item.ISRC, item.TrackPosition = "Album", "USRC17607839", domain.TrackPosition{Disc: 1, Track: 3}
	return item
}

func fillableReader() stubOwnershipReader {
	return stubOwnershipReader{owned: map[string]search.OwnedTrack{
		domain.OwnershipKey("Song", "Artist"): {TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0001", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"},
	}}
}

func ownable(kind, title, artist string, extras map[string]any) (inlibrary.OwnableItem, *map[string]any) {
	holder := &extras
	return inlibrary.OwnableItem{Kind: kind, Title: title, Artist: artist, Extras: holder}, holder
}

func TestStampOwnership_StampsOwnedTracksOnly(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewOwnership(t, bg, stubOwnershipReader{owned: map[string]search.OwnedTrack{
		domain.OwnershipKey("Song", "Artist"): {TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0002", AcquisitionStatus: "ready"},
	}}, inlibrary.AbsentTrackNumberFiller{})
	track, trackExtras := ownable("track", "Song", "Artist", nil)
	album, albumExtras := ownable("album", "Song", "Artist", map[string]any{})
	other, otherExtras := ownable("track", "Other", "Artist", map[string]any{})

	svc.StampOwnership(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{track, album, other})

	if got := (*trackExtras)["owned_track_id"]; got != "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0002" {
		t.Errorf("track owned_track_id = %v, want the owned track id", got)
	}
	if got := (*trackExtras)["owned_acquisition_status"]; got != "ready" {
		t.Errorf("track owned_acquisition_status = %v, want ready", got)
	}
	if len(*albumExtras) != 0 || len(*otherExtras) != 0 {
		t.Errorf("album %v / unowned track %v must stay unstamped", *albumExtras, *otherExtras)
	}
}

func TestStampOwnership_VersionedLibraryTrackDoesNotOwnTheStudioSong(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewOwnership(t, bg, stubOwnershipReader{owned: map[string]search.OwnedTrack{
		domain.OwnershipKey("Song (Live)", "X"):  {TrackID: "live"},
		domain.OwnershipKey("Song (Remix)", "X"): {TrackID: "remix"},
	}}, inlibrary.AbsentTrackNumberFiller{})
	studio, studioExtras := ownable("track", "Song", "X", map[string]any{})
	live, liveExtras := ownable("track", "Song (Live)", "X", map[string]any{})

	svc.StampOwnership(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{studio, live})

	if len(*studioExtras) != 0 {
		t.Errorf("studio extras = %v, want unstamped", *studioExtras)
	}
	if got := (*liveExtras)["owned_track_id"]; got != "live" {
		t.Errorf("live owned_track_id = %v, want live", got)
	}
}

func TestStampOwnership_MatchesByISRCAndRefusesAConflictingISRC(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewOwnership(t, bg, stubOwnershipReader{owned: map[string]search.OwnedTrack{
		domain.OwnershipISRCKey("USRC17607839"):    {TrackID: "by-isrc", ISRC: "USRC17607839"},
		domain.OwnershipKey("Song", "X"):           {TrackID: "other-recording", ISRC: "GBAAA0000001"},
		domain.OwnershipKey("Respelled Song", "X"): {TrackID: "by-name"},
	}}, inlibrary.AbsentTrackNumberFiller{})
	sameRecording, sameExtras := ownable("track", "Song Retitled", "Y", map[string]any{})
	sameRecording.ISRC = "us-rc1-76-07839"
	otherRecording, otherExtras := ownable("track", "Song", "X", map[string]any{})
	otherRecording.ISRC = "USRC17607839x"
	unlabelled, unlabelledExtras := ownable("track", "Respelled Song", "X", map[string]any{})
	unlabelled.ISRC = "FRZZZ0000009"

	svc.StampOwnership(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{sameRecording, otherRecording, unlabelled})

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

func TestStampOwnership_LookupFailureLeavesItemsUntouched(t *testing.T) {
	bg := searchtest.NewTracker(t)
	failing := searchtest.NewOwnership(t, bg, stubOwnershipReader{err: errors.New("db down")}, inlibrary.AbsentTrackNumberFiller{})
	item, extras := ownable("track", "Song", "Artist", nil)
	failing.StampOwnership(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{item})
	if *extras != nil {
		t.Errorf("extras = %v, want untouched after a lookup failure", *extras)
	}
}

func TestEnrichAlbumTracks_PanickingFillerIsContained(t *testing.T) {
	bg := searchtest.NewTracker(t)
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := searchtest.NewOwnership(t, bg, fillableReader(), filler)
	item := fillableItem()

	done := svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{item})

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
		bg := searchtest.NewTracker(t)
		filler := stallingFiller()
		svc := searchtest.NewOwnership(t, bg, fillableReader(), filler)
		start := time.Now()

		<-svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{fillableItem()})

		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Errorf("stalled fill returned after %v, want it abandoned at its 10s deadline", elapsed)
		}
	})
}

func TestEnrichAlbumTracks_TheSharedTrackerWaitsUntilTheFillFinishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bg := searchtest.NewTracker(t)
		filler := stallingFiller()
		svc := searchtest.NewOwnership(t, bg, fillableReader(), filler)
		item := fillableItem()
		svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{item})
		<-filler.entered

		drained := make(chan struct{})
		go func() { bg.Wait(); close(drained) }()

		synctest.Wait()
		select {
		case <-drained:
			t.Fatal("the tracker stopped waiting while the backfill was still writing")
		default:
		}
		close(filler.release)
		synctest.Wait()
		select {
		case <-drained:
		default:
			t.Fatal("the tracker never stopped waiting after the backfill finished")
		}
	})
}

func TestEnrichAlbumTracks_AFillTheTrackerRefusesCompletesWithoutWriting(t *testing.T) {
	bg := searchtest.NewTracker(t)
	if !bg.Drain(context.Background()) {
		t.Fatal("an idle tracker did not drain")
	}
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := searchtest.NewOwnership(t, bg, fillableReader(), filler)

	select {
	case <-svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{fillableItem()}):
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
	bg := searchtest.NewTracker(t)
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	svc := searchtest.NewOwnership(t, bg, stubOwnershipReader{}, filler)
	item, _ := ownable("track", "", "", map[string]any{"owned_track_id": "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0001"})
	item.TrackPosition = domain.TrackPosition{Disc: 1, Track: 3}

	select {
	case <-svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, []inlibrary.OwnableItem{item}):
	default:
		t.Fatal("expected completion handle to be closed when nothing is pending")
	}
}

type fillWrite struct {
	trackId     string
	trackNumber int
}

type recordingTrackNumberFiller struct{ writes chan fillWrite }

func (f recordingTrackNumberFiller) FillTrackNumber(_ context.Context, _ ids.UserId, trackId ids.TrackId, trackNumber int) error {
	f.writes <- fillWrite{trackId: trackId.String(), trackNumber: trackNumber}
	return nil
}

func enrichAndCollectWrites(t *testing.T, owned search.OwnedTrack, items ...inlibrary.OwnableItem) []fillWrite {
	t.Helper()
	bg := searchtest.NewTracker(t)
	filler := recordingTrackNumberFiller{writes: make(chan fillWrite, 32)}
	svc := searchtest.NewOwnership(t, bg, stubOwnershipReader{owned: map[string]search.OwnedTrack{
		domain.OwnershipKey(owned.Title, owned.Artist): owned,
	}}, filler)
	select {
	case <-svc.EnrichAlbumTracks(context.Background(), ids.UserId{}, items):
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

func positioned(album, title string, disc, position int, isrc string) inlibrary.OwnableItem {
	item, _ := ownable("track", title, "Artist", map[string]any{})
	item.Album, item.ISRC, item.TrackPosition = album, isrc, domain.TrackPosition{Disc: disc, Track: position}
	return item
}

func TestEnrichAlbumTracks_TwoDiscAlbumWritesTheProvidersPositionNotTheListIndex(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0004", Title: "Bonus One", Artist: "Artist", ISRC: "USRC17607839", Album: "Deluxe"}

	got := enrichAndCollectWrites(t, owned,
		positioned("Deluxe", "Song A", 1, 1, "USRC10000001"),
		positioned("Deluxe", "Song B", 1, 2, "USRC10000002"),
		positioned("Deluxe", "Bonus One", 2, 1, "USRC17607839"),
	)

	if len(got) != 1 || got[0] != (fillWrite{trackId: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0004", trackNumber: 1}) {
		t.Errorf("writes = %v, want only the disc 2 track = 1", got)
	}
}

func TestEnrichAlbumTracks_AMalformedOwnedTrackIdIsNeverFilled(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "not-a-uuid", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"}

	got := enrichAndCollectWrites(t, owned, positioned("Album", "Song", 1, 3, "USRC17607839"))

	if len(got) != 0 {
		t.Errorf("writes = %v, want none for an id that is not a track id", got)
	}
}

func TestEnrichAlbumTracks_CompilationPageWritesNothingOntoAStudioAlbumTrack(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0005", Title: "Hit", Artist: "Artist", ISRC: "USRC17607839", Album: "Studio Album"}

	got := enrichAndCollectWrites(t, owned, positioned("Greatest Hits", "Hit", 1, 9, "USRC17607839"))

	if len(got) != 0 {
		t.Errorf("writes = %v, want none", got)
	}
}

func TestEnrichAlbumTracks_NoWriteWithoutAnISRCMatchOrProviderPosition(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0002", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"}
	cases := map[string]inlibrary.OwnableItem{
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
	albumless := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0003", Title: "Song", Artist: "Artist", ISRC: "USRC17607839"}
	for name, pageAlbum := range map[string]string{"owned album empty, page album empty": "", "owned album empty, page album named": "Album"} {
		if got := enrichAndCollectWrites(t, albumless, positioned(pageAlbum, "Song", 1, 3, "USRC17607839")); len(got) != 0 {
			t.Errorf("%s: writes = %v, want none", name, got)
		}
	}
}

func typedItem(album, title string, position domain.TrackPosition, isrc string) inlibrary.OwnableItem {
	item, _ := ownable("track", title, "Artist", map[string]any{})
	item.Album, item.ISRC, item.TrackPosition = album, isrc, position
	return item
}

func TestEnrichAlbumTracks_WritesTheTypedTrackPositionOfTheISRCMatch(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0004", Title: "Bonus One", Artist: "Artist", ISRC: "USRC17607839", Album: "Deluxe"}

	got := enrichAndCollectWrites(t, owned,
		typedItem("Deluxe", "Song A", domain.TrackPosition{Disc: 1, Track: 1}, "USRC10000001"),
		typedItem("Deluxe", "Bonus One", domain.TrackPosition{Disc: 2, Track: 4}, "usrc-1760-7839"),
	)

	if len(got) != 1 || got[0] != (fillWrite{trackId: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0004", trackNumber: 4}) {
		t.Errorf("writes = %v, want only the disc 2 track = 4", got)
	}
}

func TestEnrichAlbumTracks_NoTypedWriteWithoutAPositionOrAnISRCMatch(t *testing.T) {
	owned := search.OwnedTrack{TrackID: "0b1f4c0e-6f3a-4a52-9a53-5f0c7a1d0002", Title: "Song", Artist: "Artist", ISRC: "USRC17607839", Album: "Album"}
	cases := map[string]inlibrary.OwnableItem{
		"unknown position": typedItem("Album", "Song", domain.TrackPosition{}, "USRC17607839"),
		"other isrc":       typedItem("Album", "Song", domain.TrackPosition{Disc: 1, Track: 3}, "GBAYE0000001"),
		"no isrc":          typedItem("Album", "Song", domain.TrackPosition{Disc: 1, Track: 3}, ""),
	}
	for name, item := range cases {
		if got := enrichAndCollectWrites(t, owned, item); len(got) != 0 {
			t.Errorf("%s: writes = %v, want none", name, got)
		}
	}
}

func TestNewOwnershipEnrichmentService_RefusesAMissingCollaborator(t *testing.T) {
	bg := searchtest.NewTracker(t)
	tests := []struct {
		name   string
		bg     *background.Tracker
		reader ports.OwnershipReader
		filler search.TrackNumberFiller
	}{
		{"background tracker", nil, stubOwnershipReader{}, inlibrary.AbsentTrackNumberFiller{}},
		{"ownership reader", bg, nil, inlibrary.AbsentTrackNumberFiller{}},
		{"track number filler", bg, stubOwnershipReader{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := search.NewOwnershipEnrichmentService(tt.bg, tt.reader, tt.filler)

			if svc != nil || !errors.Is(err, inlibrary.ErrCollaboratorRequired) {
				t.Fatalf("got (%v, %v), want a refusal wrapping inlibrary.ErrCollaboratorRequired", svc, err)
			}
		})
	}
}
