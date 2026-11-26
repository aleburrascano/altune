package inmemoryledger_test

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/tracklength"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/google/uuid"
)

const (
	servingRef = "u/the weeknd/blinding lights.mp3"
	freshRef   = "u/the weeknd/blinding lights (2).mp3"
)

func pendingTrack() inmemoryledger.LedgerTrack {
	return inmemoryledger.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
	}}
}

func readyTrack() inmemoryledger.LedgerTrack {
	track := pendingTrack()
	ref := servingRef
	track.Status, track.AudioRef = ports.TrackReady, &ref
	return track
}

func failedTrack() inmemoryledger.LedgerTrack {
	track := pendingTrack()
	track.Status, track.FailureReason = ports.TrackFailed, string(downloadfailure.FailureNoMatchFound)
	return track
}

func committed(t *testing.T, l *inmemoryledger.Ledger, id ids.TrackId) inmemoryledger.LedgerTrack {
	t.Helper()
	track, ok := l.Track(id)
	if !ok {
		t.Fatalf("track %s is not in the ledger", id)
	}
	return track
}

func complete(t *testing.T, l *inmemoryledger.Ledger, track inmemoryledger.LedgerTrack, c ports.Completion) {
	t.Helper()
	if err := l.Complete(context.Background(), track.ID, track.UserId, 0, c); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestLedger_GetReturnsTheSeededTrackToItsOwnerOnly(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track)

	got, err := ledger.Get(context.Background(), track.ID, track.UserId)
	if err != nil || got == nil || got.Title != "Blinding Lights" || !got.Streamable || got.Version != 0 {
		t.Fatalf("Get = %+v, %v, want the seeded ready track, streamable at version 0", got, err)
	}
	if other, err := ledger.Get(context.Background(), track.ID, ids.NewUserId(uuid.New())); other != nil || !errors.Is(err, ports.ErrTrackNotFound) {
		t.Errorf("Get as another user = %+v, %v, want ErrTrackNotFound", other, err)
	}
	if missing, err := ledger.Get(context.Background(), ids.NewTrackId(), track.UserId); missing != nil || !errors.Is(err, ports.ErrTrackNotFound) {
		t.Errorf("Get of an unknown track = %+v, %v, want ErrTrackNotFound", missing, err)
	}
}

func TestLedger_APendingTrackOrAReadyOneWithoutAFileIsNotStreamable(t *testing.T) {
	noFile := pendingTrack()
	noFile.Status = ports.TrackReady
	pending := pendingTrack()
	ref := servingRef
	pending.AudioRef = &ref
	ledger := inmemoryledger.NewLedger(pending, noFile)

	for _, track := range []inmemoryledger.LedgerTrack{pending, noFile} {
		if committed(t, ledger, track.ID).Streamable {
			t.Errorf("Track(%+v) reads as streamable", track)
		}
		if got, _ := ledger.Get(context.Background(), track.ID, track.UserId); got.Streamable {
			t.Errorf("Get(%+v) = streamable, want not", track)
		}
	}
}

func TestLedger_ACallerCannotChangeTheLedgerThroughWhatItReadOrSeeded(t *testing.T) {
	track := pendingTrack()
	track.RejectedSourceKeys = []string{"youtube:a"}
	track.CatalogSources = []ports.CatalogSource{{Provider: "deezer", ExternalID: "1"}}
	track.Evidence = json.RawMessage(`{"grade":"a"}`)
	ledger := inmemoryledger.NewLedger(track)
	track.RejectedSourceKeys[0], track.CatalogSources[0].Provider, track.Evidence[2] = "changed", "changed", 'X'

	got, _ := ledger.Get(context.Background(), track.ID, track.UserId)
	got.RejectedSourceKeys[0], got.CatalogSources[0].Provider, got.Evidence[2] = "changed", "changed", 'X'
	read := committed(t, ledger, track.ID)
	read.RejectedSourceKeys[0], read.CatalogSources[0].Provider, read.Evidence[2] = "changed", "changed", 'X'

	again := committed(t, ledger, track.ID)
	if again.RejectedSourceKeys[0] != "youtube:a" || again.CatalogSources[0].Provider != "deezer" || string(again.Evidence) != `{"grade":"a"}` {
		t.Errorf("ledger track = %+v, want the seeded values untouched", again)
	}
}

func everyOptional(track *inmemoryledger.LedgerTrack) []any {
	catalogSeconds, storedSeconds, mbid, explicit := 200.0, 201.0, "mbid", true
	isrc, year, number, albumArtist, genre, ref, url := "ISRC", 2020, 3, "The Weeknd", "pop", servingRef, "https://youtu.be/a"
	track.CatalogDurationSeconds, track.StoredDurationSeconds, track.CatalogMBID, track.CatalogExplicit = &catalogSeconds, &storedSeconds, &mbid, &explicit
	track.ISRC, track.Year, track.TrackNumber, track.AlbumArtist = &isrc, &year, &number, &albumArtist
	track.Genre, track.AudioRef, track.AudioSourceURL = &genre, &ref, &url
	return optionals(*track)
}

func optionals(track inmemoryledger.LedgerTrack) []any {
	return []any{
		*track.CatalogDurationSeconds, *track.StoredDurationSeconds, *track.CatalogMBID, *track.CatalogExplicit,
		*track.ISRC, *track.Year, *track.TrackNumber, *track.AlbumArtist, *track.Genre, *track.AudioRef, *track.AudioSourceURL,
	}
}

func overwriteOptionals(track inmemoryledger.LedgerTrack) {
	*track.CatalogDurationSeconds, *track.StoredDurationSeconds, *track.CatalogMBID, *track.CatalogExplicit = 1, 1, "x", false
	*track.ISRC, *track.Year, *track.TrackNumber, *track.AlbumArtist = "x", 1, 1, "x"
	*track.Genre, *track.AudioRef, *track.AudioSourceURL = "x", "x", "x"
}

func TestLedger_ACallerCannotChangeTheLedgerThroughThePointersItReadOrSeeded(t *testing.T) {
	track := pendingTrack()
	want := everyOptional(&track)
	ledger := inmemoryledger.NewLedger(track)
	overwriteOptionals(track)

	got, _ := ledger.Get(context.Background(), track.ID, track.UserId)
	overwriteOptionals(inmemoryledger.LedgerTrack{TrackSnapshot: *got})
	overwriteOptionals(committed(t, ledger, track.ID))

	if again := optionals(committed(t, ledger, track.ID)); fmt.Sprint(again) != fmt.Sprint(want) {
		t.Errorf("optionals = %v, want the seeded %v", again, want)
	}
}

func TestLedger_TrackReportsAnUnknownTrackAsMissing(t *testing.T) {
	if _, ok := inmemoryledger.NewLedger().Track(ids.NewTrackId()); ok {
		t.Error("Track of an unknown id reports it as present")
	}
}

func TestLedger_CompleteMarksAPendingTrackReadyWithEverythingTheCompletionCarries(t *testing.T) {
	track := pendingTrack()
	track.RejectedSourceKeys = []string{"youtube:old"}
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{
		AudioRef: servingRef, Duration: trackLength(t, 201.5), ISRC: "USUG11904206", Provenance: downloadprovenance.ProvenanceVerified,
		RejectedSourceKeys: []string{"youtube:old", "youtube:new", ""}, SourceURL: "https://music.youtube.com/watch?v=a",
		Confidence: 0.9, Evidence: json.RawMessage(`{"grade":"a"}`),
	})

	got := committed(t, ledger, track.ID)
	if got.Status != ports.TrackReady || got.AudioRef == nil || *got.AudioRef != servingRef || !got.Streamable || got.Version != 1 {
		t.Errorf("track = %+v, want ready on %q at version 1", got, servingRef)
	}
	if got.StoredDurationSeconds == nil || *got.StoredDurationSeconds != 201.5 || got.ISRC == nil || *got.ISRC != "USUG11904206" {
		t.Errorf("duration, isrc = %v, %v, want 201.5 and the resolved ISRC", got.StoredDurationSeconds, got.ISRC)
	}
	if got.Provenance != downloadprovenance.ProvenanceVerified || got.Confidence != 0.9 || string(got.Evidence) != `{"grade":"a"}` {
		t.Errorf("provenance, confidence, evidence = %q, %v, %s", got.Provenance, got.Confidence, got.Evidence)
	}
	if !slices.Equal(got.RejectedSourceKeys, []string{"youtube:old", "youtube:new"}) {
		t.Errorf("rejected keys = %v, want each non-empty key once", got.RejectedSourceKeys)
	}
	if got.AudioSourceURL == nil || *got.AudioSourceURL != "https://music.youtube.com/watch?v=a" {
		t.Errorf("source url = %v", got.AudioSourceURL)
	}
}

func TestLedger_CompleteLeavesWhatTheCompletionDoesNotCarry(t *testing.T) {
	track := pendingTrack()
	isrc, url, seconds := "GBAAA8000001", "https://www.youtube.com/watch?v=kept", 180.0
	track.ISRC, track.AudioSourceURL, track.StoredDurationSeconds = &isrc, &url, &seconds
	track.Provenance, track.Confidence, track.Evidence = downloadprovenance.ProvenanceCorroborated, 0.4, json.RawMessage(`{"grade":"b"}`)
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, ISRC: "USUG11904206", Provenance: "unknown"})

	got := committed(t, ledger, track.ID)
	if *got.ISRC != isrc || *got.AudioSourceURL != url || *got.StoredDurationSeconds != seconds {
		t.Errorf("isrc, url, duration = %s, %s, %v, want the stored values kept", *got.ISRC, *got.AudioSourceURL, *got.StoredDurationSeconds)
	}
	if got.Provenance != downloadprovenance.ProvenanceCorroborated || got.Confidence != 0.4 || string(got.Evidence) != `{"grade":"b"}` {
		t.Errorf("provenance, confidence, evidence = %q, %v, %s, want the stored ones kept", got.Provenance, got.Confidence, got.Evidence)
	}
}

func TestLedger_ACompletionLengthAtTheCatalogsCapIsStored(t *testing.T) {
	const week = 7 * 24 * 60 * 60.0
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track)

	atTheCap, err := tracklength.TrackLengthOf(week)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v) = %v, want the cap itself accepted", week, err)
	}
	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, Duration: atTheCap})

	if got := committed(t, ledger, track.ID); got.StoredDurationSeconds == nil || *got.StoredDurationSeconds != week {
		t.Errorf("stored length = %v, want %v: the catalog's cap is inclusive", got.StoredDurationSeconds, week)
	}
}

func TestLedger_CompleteAdoptsTheISRCWhenTheTrackHasAnEmptyOne(t *testing.T) {
	track := pendingTrack()
	empty := ""
	track.ISRC = &empty
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, ISRC: "USUG11904206"})

	if got := committed(t, ledger, track.ID); *got.ISRC != "USUG11904206" {
		t.Errorf("isrc = %q, want the resolved one", *got.ISRC)
	}
}

func TestLedger_CompleteCanMarkAReadyTrackReadyAgainOnItsOwnFile(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef})

	if got := committed(t, ledger, track.ID); got.Version != 1 {
		t.Errorf("version = %d, want the repeat to commit", got.Version)
	}
}

func TestLedger_CompleteOnAFailedTrackMarksItReadyAndClearsTheReason(t *testing.T) {
	track := failedTrack()
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef})

	if got := committed(t, ledger, track.ID); got.Status != ports.TrackReady || got.FailureReason != "" {
		t.Errorf("track = %+v, want ready with no failure reason", got)
	}
}

func TestLedger_CompleteRefusesAnotherFileOverAServingOneUnlessItReplaces(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track)

	err := ledger.Complete(context.Background(), track.ID, track.UserId, 0, ports.Completion{AudioRef: freshRef})
	if !errors.Is(err, ports.ErrIllegalTransition) {
		t.Fatalf("Complete with another file = %v, want ErrIllegalTransition", err)
	}
	if got := committed(t, ledger, track.ID); *got.AudioRef != servingRef || got.Version != 0 {
		t.Fatalf("track = %+v, want the serving file kept and nothing written", got)
	}

	complete(t, ledger, track, ports.Completion{AudioRef: freshRef, ReplaceAudio: true})
	if got := committed(t, ledger, track.ID); *got.AudioRef != freshRef {
		t.Errorf("audio ref = %q, want the replacement", *got.AudioRef)
	}
}

func TestLedger_AReplaceNeedsAServingFile(t *testing.T) {
	noFile := pendingTrack()
	noFile.Status = ports.TrackReady
	for _, track := range []inmemoryledger.LedgerTrack{pendingTrack(), failedTrack(), noFile} {
		ledger := inmemoryledger.NewLedger(track)

		err := ledger.Complete(context.Background(), track.ID, track.UserId, 0, ports.Completion{AudioRef: freshRef, ReplaceAudio: true})

		if !errors.Is(err, ports.ErrIllegalTransition) || err.Error() == ports.ErrIllegalTransition.Error() {
			t.Errorf("replace on %+v = %v, want an ErrIllegalTransition that names the operation", track, err)
		}
	}
}

func TestLedger_CompleteNeedsAnAudioRef(t *testing.T) {
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track)

	if err := ledger.Complete(context.Background(), track.ID, track.UserId, 0, ports.Completion{}); err == nil {
		t.Error("Complete without an audio ref succeeded")
	}
	if got := committed(t, ledger, track.ID); got.Status != ports.TrackPending || got.Version != 0 {
		t.Errorf("track = %+v, want it untouched", got)
	}
}

func TestLedger_ARelabelStoresTheRecordingActuallyStored(t *testing.T) {
	track := pendingTrack()
	isrc, year, seconds := "OLD000000001", 2019, 200.0
	track.ISRC, track.Year, track.CatalogDurationSeconds = &isrc, &year, &seconds
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, ISRC: "RESOLVED0001", Relabel: &ports.TrackLabel{
		MBID: "remix-mbid", Title: "Blinding Lights (Remix)", Album: "Remixes", Year: 2020, ISRC: "REMIX0000001", DurationSeconds: 222,
	}})

	got := committed(t, ledger, track.ID)
	if got.Title != "Blinding Lights (Remix)" || got.Album != "Remixes" || *got.CatalogMBID != "remix-mbid" {
		t.Errorf("labels = %q %q %v, want the remix", got.Title, got.Album, got.CatalogMBID)
	}
	if *got.Year != 2020 || *got.ISRC != "REMIX0000001" || *got.CatalogDurationSeconds != 222 {
		t.Errorf("year, isrc, length = %d %s %v, want the remix's", *got.Year, *got.ISRC, *got.CatalogDurationSeconds)
	}
}

func TestLedger_ARelabelWithoutOptionalsClearsThemAndTakesTheResolvedISRC(t *testing.T) {
	track := pendingTrack()
	isrc, year, seconds := "OLD000000001", 2019, 200.0
	track.ISRC, track.Year, track.CatalogDurationSeconds = &isrc, &year, &seconds
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, ISRC: "RESOLVED0001", Relabel: &ports.TrackLabel{MBID: "m", Title: "T"}})

	got := committed(t, ledger, track.ID)
	if got.Year != nil || got.CatalogDurationSeconds != nil || got.ISRC == nil || *got.ISRC != "RESOLVED0001" {
		t.Errorf("year, length, isrc = %v %v %v, want no year or length and the resolved ISRC", got.Year, got.CatalogDurationSeconds, got.ISRC)
	}
}

func TestLedger_FailFailsOnlyAPendingTrack(t *testing.T) {
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track, readyTrack())

	if err := ledger.Fail(context.Background(), track.ID, track.UserId, 0, "no_match_found"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if got := committed(t, ledger, track.ID); got.Status != ports.TrackFailed || got.FailureReason != "no_match_found" || got.Version != 1 {
		t.Errorf("track = %+v, want failed with the reason at version 1", got)
	}

	for _, settled := range []inmemoryledger.LedgerTrack{readyTrack(), failedTrack()} {
		ledger.Seed(settled)
		err := ledger.Fail(context.Background(), settled.ID, settled.UserId, 0, "download_failed")
		if !errors.Is(err, ports.ErrIllegalTransition) {
			t.Errorf("Fail on %v = %v, want ErrIllegalTransition", settled.Status, err)
		}
		if got := committed(t, ledger, settled.ID); got.Version != 0 {
			t.Errorf("Fail on %v wrote version %d", settled.Status, got.Version)
		}
	}
}

func TestLedger_WritesToAnUnknownTrackOrAnotherUsersTrackAreNotFound(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track)
	stranger := ids.NewUserId(uuid.New())
	ctx := context.Background()

	for name, err := range map[string]error{
		"complete unknown":   ledger.Complete(ctx, ids.NewTrackId(), track.UserId, 0, ports.Completion{AudioRef: servingRef}),
		"complete stranger":  ledger.Complete(ctx, track.ID, stranger, 0, ports.Completion{AudioRef: servingRef}),
		"fail stranger":      ledger.Fail(ctx, track.ID, stranger, 0, "x"),
		"revert unknown":     ledger.RevertAt(ctx, ports.TrackSnapshot{ID: ids.NewTrackId(), UserId: track.UserId}),
		"revert as stranger": ledger.RevertAt(ctx, ports.TrackSnapshot{ID: track.ID, UserId: stranger}),
	} {
		if !errors.Is(err, ports.ErrTrackNotFound) {
			t.Errorf("%s = %v, want ErrTrackNotFound", name, err)
		}
	}
}

func TestLedger_AWriterHoldingAnotherFenceHasLostTheLease(t *testing.T) {
	track := pendingTrack()
	track.ClaimNumber = 2
	ledger := inmemoryledger.NewLedger(track)
	ctx := context.Background()

	completeErr := ledger.Complete(ctx, track.ID, track.UserId, 1, ports.Completion{AudioRef: servingRef})
	failErr := ledger.Fail(ctx, track.ID, track.UserId, 3, "x")

	if !errors.Is(completeErr, acqdomain.ErrLeaseLost) || !errors.Is(failErr, acqdomain.ErrLeaseLost) {
		t.Errorf("stale writes = %v, %v, want ErrLeaseLost", completeErr, failErr)
	}
	if got := committed(t, ledger, track.ID); got.Version != 0 {
		t.Errorf("a stale writer committed version %d", got.Version)
	}
}

func TestLedger_TheLeaseHolderOrAWriterWithoutAFenceGoesThrough(t *testing.T) {
	cases := map[string]struct{ held, current ports.ClaimNumber }{
		"lease holder":    {held: 2, current: 2},
		"no caller fence": {held: 0, current: 2},
		"no track fence":  {held: 2, current: 0},
		"no fence at all": {held: 0, current: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			track := pendingTrack()
			track.ClaimNumber = tc.current
			ledger := inmemoryledger.NewLedger(track)

			if err := ledger.Fail(context.Background(), track.ID, track.UserId, tc.held, "x"); err != nil {
				t.Errorf("Fail = %v, want it to go through", err)
			}
		})
	}
}

func TestLedger_RevertAtRevertsAReadyOrFailedTrackAtItsSnapshotVersion(t *testing.T) {
	for _, track := range []inmemoryledger.LedgerTrack{readyTrack(), failedTrack()} {
		track.ClaimNumber = 5
		ledger := inmemoryledger.NewLedger(track)

		if err := ledger.RevertAt(context.Background(), track.TrackSnapshot); err != nil {
			t.Fatalf("RevertAt %v: %v", track.Status, err)
		}

		got := committed(t, ledger, track.ID)
		if got.Status != ports.TrackPending || got.AudioRef != nil || got.FailureReason != "" || got.Version != 1 {
			t.Errorf("track = %+v, want pending with no file or reason at version 1", got)
		}
	}
}

func TestLedger_RevertAtAStaleVersionConflictsAndWritesNothing(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track)
	complete(t, ledger, track, ports.Completion{AudioRef: servingRef})

	err := ledger.RevertAt(context.Background(), track.TrackSnapshot)

	if !errors.Is(err, inmemoryledger.ErrVersionConflict) {
		t.Errorf("RevertAt stale = %v, want ErrVersionConflict", err)
	}
	if got := committed(t, ledger, track.ID); got.Status != ports.TrackReady || got.Version != 1 {
		t.Errorf("track = %+v, want it left ready at version 1", got)
	}
}

func TestLedger_RevertAtAPendingTrackIsIllegal(t *testing.T) {
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track)

	if err := ledger.RevertAt(context.Background(), track.TrackSnapshot); !errors.Is(err, ports.ErrIllegalTransition) {
		t.Errorf("RevertAt pending = %v, want ErrIllegalTransition", err)
	}
}

func TestLedger_AFailedWriteChangesNothing(t *testing.T) {
	writeErr := errors.New("db down")
	pending, ready := pendingTrack(), readyTrack()
	ledger := inmemoryledger.NewLedger(pending, ready)
	ledger.FailWrites(writeErr)
	ctx := context.Background()

	for name, err := range map[string]error{
		"complete": ledger.Complete(ctx, pending.ID, pending.UserId, 0, ports.Completion{AudioRef: servingRef}),
		"fail":     ledger.Fail(ctx, pending.ID, pending.UserId, 0, "x"),
		"revert":   ledger.RevertAt(ctx, ready.TrackSnapshot),
	} {
		if !errors.Is(err, writeErr) {
			t.Errorf("%s = %v, want the write error", name, err)
		}
	}
	if got := committed(t, ledger, pending.ID); got.Status != ports.TrackPending || got.Version != 0 {
		t.Errorf("pending track = %+v, want it untouched", got)
	}
	if got := committed(t, ledger, ready.ID); got.Status != ports.TrackReady || got.Version != 0 {
		t.Errorf("ready track = %+v, want it untouched", got)
	}

	ledger.FailWrites(nil)
	if err := ledger.Fail(ctx, pending.ID, pending.UserId, 0, "x"); err != nil {
		t.Errorf("Fail after the store recovered = %v", err)
	}
}

func TestLedger_AnAudioRefIsInUseOnlyByAnotherTrack(t *testing.T) {
	track := readyTrack()
	ledger := inmemoryledger.NewLedger(track, pendingTrack())
	ctx := context.Background()

	if inUse, err := ledger.AudioRefInUse(ctx, servingRef, ids.NewTrackId()); !inUse || err != nil {
		t.Errorf("AudioRefInUse by another = %v, %v, want true", inUse, err)
	}
	if inUse, _ := ledger.AudioRefInUse(ctx, servingRef, track.ID); inUse {
		t.Error("a track's own file counts as in use by another")
	}
	if inUse, _ := ledger.AudioRefInUse(ctx, freshRef, ids.NewTrackId()); inUse {
		t.Error("a file no track holds counts as in use")
	}
}

func TestLedger_FailDropsAFileThePendingTrackStillPointedAtAndNeedsAReason(t *testing.T) {
	track := pendingTrack()
	ref := servingRef
	track.AudioRef = &ref
	ledger := inmemoryledger.NewLedger(track)

	if err := ledger.Fail(context.Background(), track.ID, track.UserId, 0, ""); err == nil {
		t.Fatal("Fail without a reason succeeded")
	}
	if got := committed(t, ledger, track.ID); got.Status != ports.TrackPending || got.Version != 0 {
		t.Fatalf("track = %+v, want it untouched by a reasonless fail", got)
	}
	if err := ledger.Fail(context.Background(), track.ID, track.UserId, 0, "download_failed"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if got := committed(t, ledger, track.ID); got.AudioRef != nil {
		t.Errorf("audio ref = %q, want a failed track to hold no file", *got.AudioRef)
	}
}

func TestLedger_TheRejectedSourceKeysKeepTheNewestTwentyFive(t *testing.T) {
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track)
	var keys []string
	for i := range 26 {
		keys = append(keys, fmt.Sprintf("youtube:%02d", i))
	}

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, RejectedSourceKeys: keys})

	if got := committed(t, ledger, track.ID).RejectedSourceKeys; !slices.Equal(got, keys[1:]) {
		t.Errorf("rejected keys = %v, want the newest 25", got)
	}
}

func TestLedger_AKeyPushedOutOfTheRejectedSourceKeysCanBeRejectedAgainInTheSameCompletion(t *testing.T) {
	track := pendingTrack()
	for i := range 25 {
		track.RejectedSourceKeys = append(track.RejectedSourceKeys, fmt.Sprintf("youtube:%02d", i))
	}
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, RejectedSourceKeys: []string{"youtube:new", "youtube:00"}})

	want := append(slices.Clone(track.RejectedSourceKeys[2:]), "youtube:new", "youtube:00")
	if got := committed(t, ledger, track.ID).RejectedSourceKeys; !slices.Equal(got, want) {
		t.Errorf("rejected keys = %v, want %v", got, want)
	}
}

func TestLedger_ACompletionWithAConfidenceOutOfRangeOrBrokenEvidenceKeepsTheStoredOnes(t *testing.T) {
	cases := map[string]ports.Completion{
		"above one":       {Confidence: 1.01, Evidence: json.RawMessage(`{"grade":"a"}`)},
		"below zero":      {Confidence: -0.01, Evidence: json.RawMessage(`{"grade":"a"}`)},
		"broken evidence": {Confidence: 0.9, Evidence: json.RawMessage(`{"grade":`)},
		"not a number":    {Confidence: math.NaN(), Evidence: json.RawMessage(`{"grade":"a"}`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			track := pendingTrack()
			track.Confidence, track.Evidence = 0.4, json.RawMessage(`{"grade":"b"}`)
			ledger := inmemoryledger.NewLedger(track)
			c.AudioRef = servingRef

			complete(t, ledger, track, c)

			if got := committed(t, ledger, track.ID); got.Confidence != 0.4 || string(got.Evidence) != `{"grade":"b"}` {
				t.Errorf("confidence, evidence = %v, %s, want the stored ones kept", got.Confidence, got.Evidence)
			}
		})
	}
}

func TestLedger_ACompletionAtTheConfidenceBoundsOrWithEmptyEvidenceIsStored(t *testing.T) {
	for _, c := range []ports.Completion{
		{Confidence: 0, Evidence: json.RawMessage(`{"grade":"f"}`)},
		{Confidence: 1, Evidence: json.RawMessage(`{"grade":"a"}`)},
		{Confidence: 0.5, Evidence: json.RawMessage{}},
	} {
		track := pendingTrack()
		track.Confidence, track.Evidence = 0.4, json.RawMessage(`{"grade":"b"}`)
		ledger := inmemoryledger.NewLedger(track)
		c.AudioRef = servingRef

		complete(t, ledger, track, c)

		if got := committed(t, ledger, track.ID); got.Confidence != c.Confidence || string(got.Evidence) != string(c.Evidence) {
			t.Errorf("confidence, evidence = %v, %s, want %v, %s", got.Confidence, got.Evidence, c.Confidence, c.Evidence)
		}
	}
}

func TestLedger_ARelabelTrimsItsTitleAndFallsBackToItForTheAlbum(t *testing.T) {
	track := pendingTrack()
	ledger := inmemoryledger.NewLedger(track)

	complete(t, ledger, track, ports.Completion{AudioRef: servingRef, Relabel: &ports.TrackLabel{MBID: "m", Title: "  Blinding Lights  ", Album: "  "}})

	if got := committed(t, ledger, track.ID); got.Title != "Blinding Lights" || got.Album != "Blinding Lights" {
		t.Errorf("title, album = %q, %q, want the trimmed title for both", got.Title, got.Album)
	}
}

func TestLedger_ARelabelWithoutATitleOrAnMBIDFailsTheCompletionAndWritesNothing(t *testing.T) {
	for _, label := range []ports.TrackLabel{{MBID: "m", Title: "  "}, {Title: "Blinding Lights"}} {
		track := pendingTrack()
		ledger := inmemoryledger.NewLedger(track)

		err := ledger.Complete(context.Background(), track.ID, track.UserId, 0, ports.Completion{AudioRef: servingRef, Relabel: &label})

		if err == nil {
			t.Errorf("Complete with relabel %+v succeeded", label)
		}
		if got := committed(t, ledger, track.ID); got.Status != ports.TrackPending || got.Title != "Blinding Lights" || got.Version != 0 {
			t.Errorf("track = %+v, want nothing written", got)
		}
	}
}

func trackLength(t *testing.T, seconds float64) tracklength.TrackLength {
	t.Helper()
	length, err := tracklength.TrackLengthOf(seconds)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v) = %v", seconds, err)
	}
	return length
}
