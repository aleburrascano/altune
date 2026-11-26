package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/savedlink"
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/download/sourcekey"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	acqdomain "altune/go-api/internal/download/internal/core/domain"

	"github.com/google/uuid"
)

type ctxRecordingPublisher struct {
	mu     sync.Mutex
	onLive map[events.EventType]bool
}

func newCtxRecordingPublisher() *ctxRecordingPublisher {
	return &ctxRecordingPublisher{onLive: make(map[events.EventType]bool)}
}

func (p *ctxRecordingPublisher) Publish(ctx context.Context, _ ids.UserId, payload events.Payload) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onLive[payload.EventType()] = ctx.Err() == nil
}

func (p *ctxRecordingPublisher) publishedOnLiveCtx(eventType events.EventType) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.onLive[eventType]
}

func failureCodeOf(err error) downloadfailure.FailureCode {
	return downloadfailure.FailureCode(service.FailureCodeOf(err))
}

func TestAcquireTrackAudioService_Execute_ContextEndsMidSearch_SettlesOnDetachedContext(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	source := newBlockingSource()
	pub := newCtxRecordingPublisher()
	svc := newTestAcquireService(
		liveCtxLedger{ledger}, service.NewSourceRegistry(source), newFakeAudioStore(),
		withServiceEvents(pub),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-source.searching
		cancel()
	}()

	_ = svc.Execute(ctx, userId, track.ID)

	settled := committedTrack(t, ledger, track.ID)
	if settled.Status != ports.TrackFailed {
		t.Errorf("status = %v, want %v (failed)", settled.Status, ports.TrackFailed)
	}
	if got := settled.FailureReason; got != string(downloadfailure.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
	}
	if !pub.publishedOnLiveCtx(events.TypeTrackAcquisitionFailed) {
		t.Errorf("%q was not published on a live context", events.TypeTrackAcquisitionFailed)
	}
}

func seedTwinTrack(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId, audioRef string) ids.TrackId {
	t.Helper()
	twin := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	makeReady(&twin, audioRef)
	ledger.Seed(twin)
	return twin.ID
}

func TestExecuteReplace_KeepsSupersededAudioATwinTrackStillServes(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	twinId := seedTwinTrack(t, ledger, userId, originalRef)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("object at %q = %q, want the audio track %s still serves", originalRef, got, twinId)
	}
	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Errorf("audio_ref = %v, want the replaced track moved onto its own new ref", row.AudioRef)
	}
}

func TestAcquireTrackAudioService_Execute_TrackNotFound(t *testing.T) {
	ledger := acquisitiontest.NewLedger()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	userId := ids.NewUserId(uuid.New())
	trackId := ids.NewTrackId()

	err := svc.Execute(context.Background(), userId, trackId)
	if err != nil {
		t.Fatalf("expected nil for track-not-found (silent no-op), got %v", err)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioExists(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	audioRef := "user/artist/album/song.mp3"
	makeReady(&track, audioRef)

	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)

	store := newFakeAudioStore()
	store.stored[audioRef] = true

	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	execErr := svc.Execute(context.Background(), userId, track.ID)

	if execErr != nil {
		t.Fatalf("expected nil for already-ready track with existing audio, got %v", execErr)
	}

	updated := storedTrack(t, ledger, track)
	if updated.Status != ports.TrackReady {
		t.Errorf("track status = %v, want %v (should remain ready)", updated.Status, ports.TrackReady)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioMissing(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	audioRef := "user/artist/album/song.mp3"
	makeReady(&track, audioRef)

	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)

	store := newFakeAudioStore()

	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := storedTrack(t, ledger, track)
	if updated.Status == ports.TrackReady {
		t.Error("track should not remain in 'ready' status when audio file is missing")
	}
}

func TestAcquireTrackAudioService_Execute_FailedStatus_RetriesToAcquire(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	makeFailed(&track, "previous failure reason")

	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)

	store := newFakeAudioStore()
	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := storedTrack(t, ledger, track)
	if updated.FailureReason == "previous failure reason" {
		t.Error("expected failure reason to change after retry attempt, but it remained the original")
	}
}

func queuedTrack(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId) ledgerTrack {
	t.Helper()
	track := newLedgerTrack(userId, "Fell In Love", "Lil Tecca", "")
	ledger.Seed(track)
	return track
}

func TestExecute_AlwaysSearches(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)

	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run")
	}
	if len(searcher.downloadURLs) != 0 {
		t.Errorf("no direct download should occur; got download URLs %v", searcher.downloadURLs)
	}
}

func TestExecute_PublishesStartedEvent(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")

	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)

	pub := newRecordingPublisher()
	svc := newTestAcquireService(
		ledger,
		fakeRegistry(&fakeAudioSearcher{}),
		newFakeAudioStore(),
		withServiceEvents(pub),
	)

	_ = svc.Execute(context.Background(), userId, track.ID)

	started, ok := pub.last[events.TypeTrackAcquisitionStarted]
	if !ok {
		t.Fatalf("no track_acquisition_started event published; got %+v", pub.byType)
	}
	if started["track_id"] != track.ID.String() {
		t.Fatalf("started track_id = %v, want %s", started["track_id"], track.ID.String())
	}
}

func TestExecute_WithoutConfiguredEventsDoesNotPanic(t *testing.T) {
	for name, opts := range map[string][]any{
		"no events option": nil,
	} {
		t.Run(name, func(t *testing.T) {
			userId := ids.NewUserId(uuid.New())
			track := newLedgerTrack(userId, "Song", "Artist", "Album")
			ledger := acquisitiontest.NewLedger()
			ledger.Seed(track)
			svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), opts...)

			if err := svc.Execute(context.Background(), userId, track.ID); err == nil {
				t.Fatalf("Execute with no candidates: want error, got nil")
			}
			if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
				t.Fatalf("ExecuteReplace with no candidates: want error, got nil")
			}
		})
	}
}

func TestExecute_FailureReasonResolvesToSpecificMessage(t *testing.T) {
	tests := []struct {
		name       string
		candidates []ports.AudioCandidate
	}{
		{"no candidates at all", nil},
		{"every candidate rejected (reason carries a summary)", []ports.AudioCandidate{{
			Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
			URL:     "yt:cooking",
			Channel: "CookingChannel",
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := ids.NewUserId(uuid.New())
			track := newLedgerTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
			ledger := acquisitiontest.NewLedger()
			ledger.Seed(track)
			searcher := &fakeAudioSearcher{searchResults: tt.candidates}
			svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			updated := committedTrack(t, ledger, track.ID)
			if updated.Status != ports.TrackFailed {
				t.Fatalf("track not marked failed: %+v", updated)
				return
			}
			const want = "Couldn't find this track"
			if got := downloadfailure.FailureMessage(&updated.FailureReason); got != want {
				t.Errorf("FailureMessage(%q) = %q, want %q", updated.FailureReason, got, want)
			}
		})
	}
}

func inPlaceMatch(recordings ...ports.LinkedRecording) ports.RecordingMatch {
	return ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{{ID: "ac-1", Score: 0.97, Recordings: recordings}}}
}

type stubResolver struct {
	identity ports.RecordingIdentity
	err      error
	queries  []ports.RecordingQuery
}

func (r *stubResolver) Resolve(_ context.Context, q ports.RecordingQuery) (ports.RecordingIdentity, error) {
	r.queries = append(r.queries, q)
	return r.identity, r.err
}

func TestResolveIdentity_NeverWritesTheMBLengthIntoTheTrack(t *testing.T) {
	tests := []struct {
		name     string
		identity ports.RecordingIdentity
		want     float64
	}{
		{"a trusted MB length is the expected length", ports.RecordingIdentity{Duration: 181, ISRC: "USUM71922973"}, 181},
		{"a doubted MB length is never the expected length", ports.RecordingIdentity{Duration: 181, ISRC: "USUM71922973", ReferenceDoubted: true}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{identity: tt.identity}
			ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Smaxk Or Die", Artist: "Fatt Smaxk"}}

			service.ResolveIdentity(context.Background(), resolver, ac)

			if ac.Track.CatalogDuration != 0 {
				t.Errorf("track duration = %v, want the MB length kept off the track", ac.Track.CatalogDuration)
			}
			if ac.ExpectedLength() != tt.want {
				t.Errorf("ExpectedLength() = %v, want %v", ac.ExpectedLength(), tt.want)
			}
			if ac.Track.ISRC != "" || ac.Identity.ISRC != "USUM71922973" {
				t.Errorf("track isrc %q, identity isrc %q, want the guess on the identity only", ac.Track.ISRC, ac.Identity.ISRC)
			}
		})
	}
}

func TestResolveIdentity_NeverOverwritesSavedDuration(t *testing.T) {
	resolver := &stubResolver{identity: ports.RecordingIdentity{Duration: 999}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "T", Artist: "A", CatalogDuration: 200}}

	service.ResolveIdentity(context.Background(), resolver, ac)

	if ac.Track.CatalogDuration != 200 {
		t.Errorf("duration = %v, want the saved 200 kept", ac.Track.CatalogDuration)
	}
}

func TestResolveIdentity_ResolverErrorIsNonFatal(t *testing.T) {
	resolver := &stubResolver{err: errors.New("discovery down")}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "T", Artist: "A", CatalogDuration: 200}}

	service.ResolveIdentity(context.Background(), resolver, ac)

	if ac.Track.CatalogDuration != 200 || !ac.Identity.IsZero() {
		t.Errorf("a resolver failure must leave the track untouched, got %+v", ac.Track)
	}
}

func TestResolveIdentity_PassesTrackMetadataToResolver(t *testing.T) {
	stub := &stubResolver{}
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding", ISRC: "X",
	}}

	service.ResolveIdentity(context.Background(), stub, ac)

	if len(stub.queries) != 1 {
		t.Fatalf("resolver calls = %d, want 1", len(stub.queries))
	}
	got := stub.queries[0]
	want := ports.RecordingQuery{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding", ISRC: "X"}
	if got != want {
		t.Errorf("query = %+v, want %+v", got, want)
	}
}

func TestExecute_WithoutResolverStillRuns(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger.Seed(track)

	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)
}

func TestExecute_AnExistsCheckErrorOnAReadyTrackPreservesItsAudioRef(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	audioRef := "user/artist/album/song.mp3"
	makeReady(&track, audioRef)

	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)

	store := newFakeAudioStore()
	store.err = errors.New("transient s3 hiccup")

	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), store)

	err := svc.Execute(context.Background(), userId, track.ID)

	if err == nil {
		t.Fatal("expected Execute to return the exists-check error, got nil")
	}
	stored := committedTrack(t, ledger, track.ID)
	if stored.Status != ports.TrackReady {
		t.Errorf("status = %v, want %v (must stay ready on transient error)", stored.Status, ports.TrackReady)
	}
	if stored.AudioRef == nil || *stored.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q preserved (a transient error is not evidence the file is gone)", stored.AudioRef, audioRef)
	}
}

type bytesAudioStore struct {
	objects   map[string]string
	deleteErr error
}

func (s *bytesAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	_, ok := s.objects[audioRef]
	return ok, nil
}

func (s *bytesAudioStore) Store(_ context.Context, sourcePath string, audioRef string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	s.objects[audioRef] = string(data)
	return nil
}

func (s *bytesAudioStore) Delete(_ context.Context, audioRef string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, audioRef)
	return nil
}

type newAudioSource struct{}

func (newAudioSource) Name() acqdomain.SourceName { return "new-audio" }

func (newAudioSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        "https://www.youtube.com/watch?v=freshAAAAAA",
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
	}}, nil
}

func (newAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return fetchedFrom(path, os.WriteFile(path, []byte("new bytes"), 0o600))
}

func trackRefOf(track ledgerTrack) service.TrackRef {
	return service.TrackRef{
		ID:     track.ID.String(),
		UserID: track.UserId.String(),
		Title:  track.Title,
		Artist: track.Artist,
		Album:  track.Album,
	}
}

func seedReadyTrackWith(t *testing.T) (*acquisitiontest.Ledger, *bytesAudioStore, ids.UserId, ids.TrackId, string) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	originalRef := service.BuildAudioRef(trackRefOf(track), "audio.mp3")
	makeReady(&track, originalRef)
	useSource(&track, "https://www.youtube.com/watch?v=currentAAAA")

	ledger := acquisitiontest.NewLedger(track)
	store := &bytesAudioStore{objects: map[string]string{originalRef: "original bytes"}}
	return ledger, store, userId, track.ID, originalRef
}

func TestExecuteReplace_FailedCommitKeepsTheOriginalAudioIntact(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	ledger.FailWrites(errors.New("transient db error"))
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("expected the replace to fail when the track update cannot commit")
	}

	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("object at the serving ref = %q, want the original audio untouched", got)
	}
	if len(store.objects) != 1 {
		t.Errorf("objects = %v, want the failed attempt's staged audio rolled back", store.objects)
	}
	row := committedTrack(t, ledger, trackId)
	if row.Status != ports.TrackReady || row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it still ready on the original ref", row.Status, row.AudioRef)
	}
}

func TestExecuteReplace_CommittedSwapPointsAtTheNewAudioAndDropsTheOld(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Fatalf("audio_ref = %v, want a new per-attempt ref distinct from %q", row.AudioRef, originalRef)
	}
	if got := store.objects[*row.AudioRef]; got != "new bytes" {
		t.Errorf("object at the committed ref = %q, want the replacement audio", got)
	}
	if _, stillThere := store.objects[originalRef]; stillThere {
		t.Error("the superseded audio must be deleted once the swap has committed")
	}
	if filepath.Ext(*row.AudioRef) != ".mp3" {
		t.Errorf("audio_ref %q lost its extension, which drives the served content type", *row.AudioRef)
	}
}

func TestExecuteReplace_OldAudioDeleteFailureDoesNotUndoTheSwap(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	store.deleteErr = errors.New("object storage unavailable")
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the committed swap reported as success", err)
	}

	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || *row.AudioRef == originalRef || store.objects[*row.AudioRef] != "new bytes" {
		t.Errorf("audio_ref = %v, want the committed ref serving the replacement", row.AudioRef)
	}
}

func TestExecute_FreshAcquisitionKeepsTheCanonicalRef(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	ledger := acquisitiontest.NewLedger(track)
	store := &bytesAudioStore{objects: map[string]string{}}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	if err := svc.Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	row := committedTrack(t, ledger, track.ID)
	want := service.BuildAudioRef(trackRefOf(track), "audio.mp3")
	if row.AudioRef == nil || *row.AudioRef != want {
		t.Errorf("audio_ref = %v, want the canonical %q", row.AudioRef, want)
	}
}

func readyTrackWithSource(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId, ref, source string) ledgerTrack {
	t.Helper()
	track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	makeReady(&track, ref)
	useSource(&track, source)
	ledger.Seed(track)
	return track
}

func TestExecuteReplace_ExcludesTheStoredSource(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("replace must run the search pipeline rather than skipping a ready track")
	}
}

func TestExecuteReplace_FailureLeavesTheTrackReady(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
		t.Fatal("expected the replace to fail with no candidates")
	}

	updated := storedTrack(t, ledger, track)
	if updated.Status != ports.TrackReady {
		t.Errorf("status = %v, want the track left ready", updated.Status)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "u/a/b/c.mp3" {
		t.Errorf("audio_ref = %v, want the original preserved", updated.AudioRef)
	}
	if !store.stored["u/a/b/c.mp3"] {
		t.Error("a failed replace must never delete the audio the user still has")
	}
}

func TestExecute_NonReplaceStillSkipsAReadyTrack(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if searcher.searchCalled {
		t.Error("ordinary acquire must still no-op on a ready track whose audio exists")
	}
}

func TestExecuteReplace_LegacyTrackSkipsTopRankedInsteadOfExcluding(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	updated := storedTrack(t, ledger, track)
	if updated.Status != ports.TrackReady {
		t.Errorf("status = %v, want the track left ready", updated.Status)
	}
}

type cancellingSource struct {
	*fakeAudioSearcher
	cancel func()
}

func (s *cancellingSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.cancel()
	return nil, errors.New("upstream closed")
}

func TestExecute_CancelledMidSearch_PersistsCancellation(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancellingSource{fakeAudioSearcher: &fakeAudioSearcher{}, cancel: cancel}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(source), newFakeAudioStore())

	_ = svc.Execute(ctx, userId, track.ID)

	if got := committedTrack(t, ledger, track.ID).FailureReason; got != string(downloadfailure.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
	}
}

func seedTrackInRepo(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId, settle func(*ledgerTrack)) ledgerTrack {
	t.Helper()
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	if settle != nil {
		settle(&track)
	}
	ledger.Seed(track)
	return track
}

func storedTrack(t *testing.T, ledger *acquisitiontest.Ledger, track ledgerTrack) ledgerTrack {
	t.Helper()
	return committedTrack(t, ledger, track.ID)
}

func TestAcquire_StaleFailureDoesNotOverwriteReadyTrack(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := seedTrackInRepo(t, ledger, userId, readyWith("u/a/b/good.opus"))
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.RefuseQueued(context.Background(), userId, track.ID)

	got := storedTrack(t, ledger, track)
	if got.Status != ports.TrackReady {
		t.Fatalf("status = %v, want ready: a stale failure overwrote a completed acquisition", got.Status)
	}
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/good.opus" {
		t.Errorf("AudioRef = %v, want the concurrent success's audio kept", got.AudioRef)
	}
}

func TestAcquire_DuplicateFailureKeepsOriginalReason(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := seedTrackInRepo(t, ledger, userId, func(tr *ledgerTrack) {
		makeFailed(tr, string(downloadfailure.FailureAcquisitionInterrupted))
	})
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.RefuseQueued(context.Background(), userId, track.ID)

	got := storedTrack(t, ledger, track)
	if got.FailureReason != string(downloadfailure.FailureAcquisitionInterrupted) {
		t.Errorf("FailureReason = %v, want the first failure's reason kept", got.FailureReason)
	}
}

func TestAcquire_FailureOnPendingTrackStillMarksFailed(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := seedTrackInRepo(t, ledger, userId, nil)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.RefuseQueued(context.Background(), userId, track.ID)

	got := storedTrack(t, ledger, track)
	if got.Status != ports.TrackFailed {
		t.Errorf("status = %v, want failed", got.Status)
	}
}

func TestResolveIdentity_QueryCarriesTheTracksDuration(t *testing.T) {
	stub := &stubResolver{}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Circles", Artist: "Post Malone", CatalogDuration: 215}}

	service.ResolveIdentity(context.Background(), stub, ac)

	if len(stub.queries) != 1 || stub.queries[0].Duration != 215 {
		t.Errorf("queries = %+v, want one carrying Duration 215", stub.queries)
	}
}

type fakeRejectionStore struct {
	active    []ports.PriorRejection
	activeErr error
	recordErr error
	recorded  []ports.CandidateRejectionRecord
	since     time.Time
}

func (f *fakeRejectionStore) Record(_ context.Context, recs []ports.CandidateRejectionRecord) error {
	f.recorded = append(f.recorded, recs...)
	return f.recordErr
}

func (f *fakeRejectionStore) Active(_ context.Context, _ string, since time.Time) ([]ports.PriorRejection, error) {
	f.since = since
	return f.active, f.activeErr
}

func priorRejectionsOnTheRunsBasis(urls ...string) []ports.PriorRejection {
	prior := make([]ports.PriorRejection, len(urls))
	for i, u := range urls {
		prior[i] = ports.PriorRejection{SourceKey: sourcekey.SourceKey(u), Basis: &ports.RejectionBasis{}}
	}
	return prior
}

const (
	drmURL   = "https://soundcloud.com/bran-van-3000/drinking-in-l-a-3"
	otherURL = "https://youtube.com/watch?v=dQw4w9WgXcQ"
)

func namedCandidate(url string) ports.AudioCandidate {
	return ports.AudioCandidate{Title: "Lil Tecca - Fell In Love", URL: url, Source: "fake"}
}

func retryWith(t *testing.T, store ports.RejectionStore, urls ...string) (*fakeAudioSearcher, *acquisitiontest.Ledger, ids.TrackId) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(), withServiceRejections(store))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher, ledger, track.ID
}

func TestExecute_RetrySkipsAURLRejectedAsDRMTenDaysAgo(t *testing.T) {
	store := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}

	searcher, _, _ := retryWith(t, store, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
	if got := time.Since(store.since); got < 29*24*time.Hour || got > 31*24*time.Hour {
		t.Errorf("active keys asked since %v ago, want about 30 days", got)
	}
}

func TestExecute_RetryAttemptsAPriorRejectionWhenItIsTheOnlyCandidate(t *testing.T) {
	store := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}

	searcher, _, _ := retryWith(t, store, drmURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != drmURL {
		t.Errorf("download attempts = %v, want the only candidate %q", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_ASearchLengthRejectionIsNotRemembered(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)
	track = withCatalogLength(t, ledger, track, 200)
	audio := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	short := namedCandidate("https://youtube.com/watch?v=shortshort1")
	short.Duration = 30
	downloaded := namedCandidate(otherURL)
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{short, downloaded}, downloadPath: audio}
	store := &fakeRejectionStore{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(),
		withServiceRejections(store), withServiceProber(&queueProber{durations: []float64{30}}))

	_ = svc.Execute(context.Background(), userId, track.ID)

	if slices.Contains(searcher.downloadURLs, short.URL) {
		t.Fatalf("download attempts = %v, the search-length candidate must never be downloaded", searcher.downloadURLs)
	}
	if len(store.recorded) != 1 || store.recorded[0].Reason != "duration" || store.recorded[0].SourceKey != sourcekey.SourceKey(downloaded.URL) {
		t.Fatalf("recorded = %+v, want only the probed duration rejection for %q", store.recorded, downloaded.URL)
	}
	if got := store.recorded[0].TrackID; got != track.ID.String() {
		t.Errorf("recorded track = %q, want the run's own track %q so the next run finds the row", got, track.ID.String())
	}
}

const (
	recordedLength = 200.0
	runMBID        = "11111111-2222-3333-4444-555555555555"
)

func retryOnALength(t *testing.T, prior []ports.PriorRejection, urls ...string) (*fakeAudioSearcher, *fakeRejectionStore) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)
	track = withCatalogLength(t, ledger, track, recordedLength)
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	store := &fakeRejectionStore{active: prior}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(),
		withServiceRejections(store), withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher, store
}

func priorRejectionOn(url string, basis *ports.RejectionBasis) []ports.PriorRejection {
	return []ports.PriorRejection{{SourceKey: sourcekey.SourceKey(url), Basis: basis}}
}

func TestExecute_APriorRejectionJudgedOnAnotherLengthIsRetried(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: runMBID})

	searcher, _ := retryOnALength(t, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: its rejection was judged on 150s, the run expects %vs", searcher.downloadURLs, drmURL, recordedLength)
	}
}

func TestExecute_APriorRejectionOnTheSameBasisIsSkipped(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: recordedLength, ReferenceMBID: runMBID})

	searcher, _ := retryOnALength(t, prior, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
}

func TestExecute_APriorRejectionWithNoBasisIsIgnored(t *testing.T) {
	searcher, _ := retryOnALength(t, priorRejectionOn(drmURL, nil), drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: its rejection has no recorded basis", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_APriorRejectionJudgedOnAnotherRecordingIsRetried(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: recordedLength, ReferenceMBID: "another-recording"})

	searcher, _ := retryOnALength(t, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: its rejection was judged against another MBID", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_RecordsTheBasisWithEachLastingRejection(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)
	track = withCatalogLength(t, ledger, track, recordedLength)
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{namedCandidate(otherURL)}, downloadPath: audio}
	store := &fakeRejectionStore{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(),
		withServiceRejections(store), withServiceProber(&queueProber{durations: []float64{30}}), withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))

	_ = svc.Execute(context.Background(), userId, track.ID)

	want := ports.RejectionBasis{ExpectedSeconds: recordedLength, ReferenceMBID: runMBID}
	if len(store.recorded) != 1 || store.recorded[0].Basis != want {
		t.Errorf("recorded = %+v, want one rejection carrying basis %+v", store.recorded, want)
	}
}

func TestExecute_FailingRejectionStoreNeverChangesTheOutcome(t *testing.T) {
	store := &fakeRejectionStore{activeErr: errors.New("db down"), recordErr: errors.New("db down")}

	searcher, ledger, trackId := retryWith(t, store, otherURL)

	if len(searcher.downloadURLs) != 1 {
		t.Errorf("download attempts = %v, want the candidate still tried", searcher.downloadURLs)
	}
	if got := committedTrack(t, ledger, trackId).Status; got != ports.TrackFailed {
		t.Errorf("status = %v, want failed from the download, not the store", got)
	}
}

func TestExecuteReplace_ExcludesStoredKeysAndRejectedSourceKeys(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")
	track.RejectedSourceKeys = []string{sourcekey.SourceKey("https://youtube.com/watch?v=earlier0000")}
	ledger.Seed(track)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{"https://youtube.com/watch?v=earlier0000", drmURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	rejections := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store, withServiceRejections(rejections))

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
}

func transientSearchFailure() *fakeAudioSearcher {
	return &fakeAudioSearcher{searchErr: &acqdomain.SourceUnavailableError{Source: "fake", Err: errors.New("http error 503")}}
}

func executeOnAttempt(t *testing.T, searcher *fakeAudioSearcher, attempt int) (ledgerTrack, *recordingPublisher, error) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(), withServiceEvents(pub))
	ctx := service.WithJobRun(context.Background(), service.JobRun{Attempt: attempt, SchedulerOwned: true})

	execErr := svc.Execute(ctx, userId, track.ID)

	return storedTrack(t, ledger, track), pub, execErr
}

func TestExecute_TransientFailureBelowTheAttemptCap_ReturnsRetryableAndLeavesTheTrackPending(t *testing.T) {
	for attempt := 1; attempt < service.MaxAcquisitionAttempts; attempt++ {
		stored, pub, execErr := executeOnAttempt(t, transientSearchFailure(), attempt)
		if pub == nil {
			t.Fatal("no publisher")
		}

		if !errors.Is(execErr, service.ErrAcquisitionRetryable) {
			t.Errorf("attempt %d: err = %v, want ErrAcquisitionRetryable", attempt, execErr)
		}
		if stored.Status != ports.TrackPending {
			t.Errorf("attempt %d: status = %v, want pending", attempt, stored.Status)
		}
		if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
			t.Errorf("attempt %d: failed publishes = %d, want 0", attempt, got)
		}
	}
}

func TestExecute_TransientFailureOnTheLastAttempt_SettlesFailedAsSourceUnavailable(t *testing.T) {
	stored, pub, execErr := executeOnAttempt(t, transientSearchFailure(), service.MaxAcquisitionAttempts)
	if pub == nil {
		t.Fatal("no publisher")
	}

	if errors.Is(execErr, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", execErr)
	}
	if stored.Status != ports.TrackFailed {
		t.Fatalf("status = %v, want failed", stored.Status)
	}
	if got := stored.FailureReason; got != string(downloadfailure.FailureSourceUnavailable) {
		t.Errorf("failure reason = %q, want %q", got, downloadfailure.FailureSourceUnavailable)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 1 {
		t.Errorf("failed publishes = %d, want 1", got)
	}
}

func TestExecute_PermanentFailureOnTheFirstAttempt_SettlesFailedAsNoMatch(t *testing.T) {
	stored, _, execErr := executeOnAttempt(t, &fakeAudioSearcher{}, 1)

	if errors.Is(execErr, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", execErr)
	}
	if stored.Status != ports.TrackFailed {
		t.Fatalf("status = %v, want failed", stored.Status)
	}
	if got := stored.FailureReason; !strings.HasPrefix(got, string(downloadfailure.FailureNoMatchFound)) {
		t.Errorf("failure reason = %q, want prefix %q", got, downloadfailure.FailureNoMatchFound)
	}
}

type unknownTopicSource struct{}

func (unknownTopicSource) Name() acqdomain.SourceName { return "unknown-topic" }

func (unknownTopicSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        "https://www.youtube.com/watch?v=topicAAAAAA",
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
		Duration:   200,
	}}, nil
}

func (unknownTopicSource) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	fetched, err := newAudioSource{}.Fetch(ctx, c, outDir)
	fetched.Format = ports.SourceFormat{ID: "140", Codec: "mp4a.40.2", Kbps: 128}
	return fetched, err
}

func acquireUnknownTopicWithFloor(t *testing.T, opts ...any) (ledgerTrack, error) {
	t.Helper()
	return acquireUnknownTopicIn(t, context.Background(), opts...)
}

func acquireUnknownTopicIn(t *testing.T, ctx context.Context, opts ...any) (ledgerTrack, error) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	seconds := 200.0
	track.StoredDurationSeconds = &seconds
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(unknownTopicSource{}),
		&bytesAudioStore{objects: map[string]string{}},
		append([]any{
			withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding"}}),
			withServiceIdentifier(&stubIdentifier{}),
		}, opts...)...)
	err := svc.Execute(ctx, userId, track.ID)
	return storedTrack(t, ledger, track), err
}

func TestExecute_FloorAboveTheCandidatesConfidenceFailsAsNoConfidentMatch(t *testing.T) {
	track, err := acquireUnknownTopicWithFloor(t, service.WithAcquireConfidenceFloor(0.9))
	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
	if track.Status != ports.TrackFailed {
		t.Errorf("status = %v, want failed", track.Status)
	}
}

func TestExecute_DefaultFloorStoresTheSameCandidate(t *testing.T) {
	track, err := acquireUnknownTopicWithFloor(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if track.Status != ports.TrackReady {
		t.Errorf("status = %v, want ready", track.Status)
	}
}

type m4aAudioSource struct{ newAudioSource }

func (m4aAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.m4a")
	return fetchedFrom(path, os.WriteFile(path, []byte("aac bytes"), 0o600))
}

func TestExecuteReplace_AnM4AReplacesAnOldMP3AndDropsIt(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(m4aAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || filepath.Ext(*row.AudioRef) != ".m4a" {
		t.Fatalf("audio_ref = %v, want an .m4a key", row.AudioRef)
	}
	if got := store.objects[*row.AudioRef]; got != "aac bytes" {
		t.Errorf("object at the committed ref = %q, want the m4a audio", got)
	}
	if _, stillThere := store.objects[originalRef]; stillThere {
		t.Errorf("old mp3 %q still stored, want it deleted after the swap", originalRef)
	}
}

type brokenArtTrackSource struct{ finds int }

func (*brokenArtTrackSource) Name() acqdomain.SourceName { return "ytmsearch" }

func (s *brokenArtTrackSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.finds++
	return []ports.AudioCandidate{{
		Title: "Blinding Lights",
		URL:   "https://music.youtube.com/watch?v=artTrackAAA",
		Kind:  ports.KindArtTrack,
	}}, nil
}

func (*brokenArtTrackSource) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("yt-dlp download: exit status 1")
}

func TestExecuteReplace_AFailedFirstTierDownloadFallsBackToTheTextSearch(t *testing.T) {
	ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
	first := &brokenArtTrackSource{}
	registry := service.NewSourceRegistry(first).WithFallback(newAudioSource{})
	svc := newTestAcquireService(ledger, registry, store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the fallback candidate to succeed", err)
	}
	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || store.objects[*row.AudioRef] != "new bytes" {
		t.Fatalf("stored audio = %v, want the fallback source's audio", row.AudioRef)
	}
	if first.finds != 1 {
		t.Errorf("first tier searched %d times, want 1 (the retry asks only the fallback)", first.finds)
	}
}

func TestExecuteReplace_NoFallbackRetryWhenTheFallbackAlreadyRan(t *testing.T) {
	ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
	unresolvedBroken := &findStubSource{name: "ytdlp", found: []ports.AudioCandidate{titledCandidate("https://www.youtube.com/watch?v=brokenAAAAA")}, fetchErr: errors.New("exit status 1")}
	first := &findStubSource{name: "ytmsearch"}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(first).WithFallback(unresolvedBroken), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("ExecuteReplace = nil, want the download failure")
	}
	if unresolvedBroken.finds != 1 {
		t.Errorf("fallback searched %d times, want exactly 1", unresolvedBroken.finds)
	}
}

func TestExecuteReplace_FailsCleanlyWhenNoFactIdentifiesTheCurrentFile(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	row := committedTrack(t, ledger, trackId)
	row.AudioSourceURL = nil
	row.StoredDurationSeconds = nil
	ledger.Seed(row)
	source := &findStubSource{name: "ytmsearch", found: []ports.AudioCandidate{titledCandidate("https://www.youtube.com/watch?v=otherAAAAAA")}}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(source), store)

	err := svc.ExecuteReplace(context.Background(), userId, trackId)

	if !errors.Is(err, service.ErrReplaceFactMissing) {
		t.Fatalf("ExecuteReplace = %v, want ErrReplaceFactMissing", err)
	}
	if errors.Is(err, service.ErrAcquisitionRetryable) {
		t.Error("a missing fact is not retryable")
	}
	if source.finds != 0 {
		t.Errorf("sources searched %d times, want 0 (the replace must not run blind)", source.finds)
	}
	if got := store.objects[originalRef]; got != "original bytes" || len(store.objects) != 1 {
		t.Errorf("store = %v, want only the original audio untouched", store.objects)
	}
	if got := committedTrack(t, ledger, trackId).AudioRef; got == nil || *got != originalRef {
		t.Errorf("track audio ref = %v, want %q kept", got, originalRef)
	}
}

type lengthRecordingSource struct {
	newAudioSource
	requested []float64
}

func (s *lengthRecordingSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.requested = append(s.requested, req.Duration)
	return s.newAudioSource.Find(ctx, req)
}

func TestExecuteReplace_TheStoredLengthIsNotTheTarget(t *testing.T) {
	ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
	row := committedTrack(t, ledger, trackId)
	wrongVersion := 112.0
	row.StoredDurationSeconds = &wrongVersion
	ledger.Seed(row)
	resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: "e7ca1ccd-65cf-43e3-9964-ffb5a0479077", Duration: 128}}
	source := &lengthRecordingSource{}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(source), store, withServiceResolver(resolver))

	_ = svc.ExecuteReplace(context.Background(), userId, trackId)

	if len(resolver.queries) != 1 || resolver.queries[0].Duration != 0 {
		t.Errorf("resolver queries = %+v, want one with no length: the stored 112 s is not a target", resolver.queries)
	}
	if len(source.requested) == 0 || source.requested[0] != 128 {
		t.Errorf("search lengths = %v, want the MusicBrainz 128 s, not the stored 112 s", source.requested)
	}
}

type storedBytesReader struct {
	store *bytesAudioStore
	err   error
}

func (r storedBytesReader) CopyStored(_ context.Context, audioRef, dir string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	path := filepath.Join(dir, "stored"+filepath.Ext(audioRef))
	return path, os.WriteFile(path, []byte(r.store.objects[audioRef]), 0o600)
}

type contentRecordingIdentifier struct {
	mu       sync.Mutex
	contents []string
	storedID string
}

func (i *contentRecordingIdentifier) Identify(_ context.Context, path string, _ float64) (ports.RecordingMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.contents = append(i.contents, string(data))
	if string(data) == "original bytes" {
		return ports.RecordingMatch{AcoustID: i.storedID}, nil
	}
	return ports.RecordingMatch{}, nil
}

func (i *contentRecordingIdentifier) identifiedStoredFile() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := 0
	for _, c := range i.contents {
		if c == "original bytes" {
			n++
		}
	}
	return n
}

func captureServiceLogs(t *testing.T) *strings.Builder {
	t.Helper()
	var out strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

func seedUnknownSourceTrack(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId, trackId ids.TrackId) {
	t.Helper()
	row := committedTrack(t, ledger, trackId)
	stored := 112.0
	row.AudioSourceURL = nil
	row.StoredDurationSeconds = &stored
	ledger.Seed(row)
}

func TestExecuteReplace_FingerprintsTheStoredFileOnce(t *testing.T) {
	tests := map[string]struct {
		clearSource bool
		wantCalls   int
		wantLogged  bool
	}{
		"no source url identifies the stored file once": {clearSource: true, wantCalls: 1, wantLogged: true},
		"a known source url does not":                   {clearSource: false, wantCalls: 0, wantLogged: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
			if tt.clearSource {
				seedUnknownSourceTrack(t, ledger, userId, trackId)
			}
			identifier := &contentRecordingIdentifier{storedID: "acoustid-of-the-stored-file"}
			logs := captureServiceLogs(t)
			svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store,
				withServiceIdentifier(identifier), withServiceStoredAudio(storedBytesReader{store: store}))

			if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
				t.Fatalf("ExecuteReplace: %v", err)
			}

			if got := identifier.identifiedStoredFile(); got != tt.wantCalls {
				t.Errorf("stored file identified %d times, want %d", got, tt.wantCalls)
			}
			logged := strings.Contains(logs.String(), "acquisition.replace_current_file_fingerprinted") &&
				strings.Contains(logs.String(), "stored_acoustid=acoustid-of-the-stored-file")
			if logged != tt.wantLogged {
				t.Errorf("fingerprint of the stored file logged = %v, want %v", logged, tt.wantLogged)
			}
		})
	}
}

func TestExecuteReplace_AFailedStoredCopyIsLoggedAndTheReplaceStillRuns(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	seedUnknownSourceTrack(t, ledger, userId, trackId)
	identifier := &contentRecordingIdentifier{storedID: "never-recorded"}
	logs := captureServiceLogs(t)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store,
		withServiceIdentifier(identifier), withServiceStoredAudio(storedBytesReader{store: store, err: errors.New("object storage unavailable")}))

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the replace to carry on without the stored fingerprint", err)
	}

	if !strings.Contains(logs.String(), "acquisition.replace_current_file_fingerprint_failed") {
		t.Errorf("logs = %q, want the failed stored copy reported", logs.String())
	}
	if strings.Contains(logs.String(), "acquisition.replace_current_file_fingerprinted") {
		t.Error("a failed copy must not log a fingerprint")
	}
	row := committedTrack(t, ledger, trackId)
	if row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Errorf("audio_ref = %v, want the replace to have swapped in the new audio", row.AudioRef)
	}
}

func TestExecuteReplace_TheStoredCopyWorkspaceIsRemovedWhateverTheOutcome(t *testing.T) {
	tests := map[string]error{
		"after a successful identify": nil,
		"after a failed copy":         errors.New("object storage unavailable"),
	}
	for name, copyErr := range tests {
		t.Run(name, func(t *testing.T) {
			ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
			seedUnknownSourceTrack(t, ledger, userId, trackId)
			ws := newCreationRecordingWorkspace()
			svc := newTestAcquireServiceIn(ws, ledger, service.NewSourceRegistry(newAudioSource{}), store,
				withServiceIdentifier(&contentRecordingIdentifier{}),
				withServiceStoredAudio(storedBytesReader{store: store, err: copyErr}))

			if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
				t.Fatalf("ExecuteReplace: %v", err)
			}

			storedDirs := 0
			for _, dir := range ws.createdDirs() {
				if !strings.Contains(dir, "stored-") {
					continue
				}
				storedDirs++
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Errorf("stored copy dir %s still exists (stat err = %v)", dir, err)
				}
			}
			if storedDirs != 1 {
				t.Errorf("stored copy dirs created = %d, want 1", storedDirs)
			}
		})
	}
}

type uploadsByBytesSource struct {
	mu      sync.Mutex
	uploads map[string]string
	found   []ports.AudioCandidate
	fetched []string
}

func (s *uploadsByBytesSource) Name() acqdomain.SourceName { return "uploads" }

func (s *uploadsByBytesSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.found, nil
}

func (s *uploadsByBytesSource) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, c.URL)
	s.mu.Unlock()
	path := filepath.Join(outDir, "audio.mp3")
	return fetchedFrom(path, os.WriteFile(path, []byte(s.uploads[c.URL]), 0o600))
}

type acoustIDByBytesIdentifier struct {
	byBytes map[string]ports.RecordingMatch
}

func (i acoustIDByBytesIdentifier) Identify(_ context.Context, path string, _ float64) (ports.RecordingMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	return i.byBytes[string(data)], nil
}

const (
	sameLengthCurrentURL = "https://www.youtube.com/watch?v=currentFile"
	sameLengthOtherURL   = "https://www.youtube.com/watch?v=otherUpload"
	recordingMBID        = "e7ca1ccd-65cf-43e3-9964-ffb5a0479077"
)

func sameLengthUploads() *uploadsByBytesSource {
	return &uploadsByBytesSource{
		uploads: map[string]string{sameLengthCurrentURL: "the current file again", sameLengthOtherURL: "another upload"},
		found: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: sameLengthCurrentURL, Channel: "The Weeknd - Topic", Categories: []string{"Music"}, Duration: 112, ViewCount: 9000000},
			{Title: "Blinding Lights", URL: sameLengthOtherURL, Channel: "The Weeknd - Topic", Categories: []string{"Music"}, Duration: 112, ViewCount: 1000},
		},
	}
}

func replaceOfAnUnknownSource(t *testing.T, source *uploadsByBytesSource, identifier acoustIDByBytesIdentifier, readerFor func(*bytesAudioStore) ports.StoredAudioReader) (*bytesAudioStore, error) {
	t.Helper()
	ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
	seedUnknownSourceTrack(t, ledger, userId, trackId)
	resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: recordingMBID, Duration: 112}}
	options := []any{withServiceResolver(resolver), withServiceIdentifier(identifier)}
	if readerFor != nil {
		options = append(options, withServiceStoredAudio(readerFor(store)))
	}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(source), store, options...)
	return store, svc.ExecuteReplace(context.Background(), userId, trackId)
}

func storesBytes(store *bytesAudioStore, want string) bool {
	for _, stored := range store.objects {
		if stored == want {
			return true
		}
	}
	return false
}

func TestExecuteReplace_TheCurrentFileIsFoundByFingerprint(t *testing.T) {
	source := sameLengthUploads()
	identifier := acoustIDByBytesIdentifier{byBytes: map[string]ports.RecordingMatch{
		"original bytes":         {AcoustID: "acoustid-of-the-stored-file", MBIDs: []string{recordingMBID}},
		"the current file again": {AcoustID: "acoustid-of-the-stored-file", MBIDs: []string{recordingMBID}},
		"another upload":         {AcoustID: "acoustid-of-another-upload", MBIDs: []string{recordingMBID}},
	}}

	store, err := replaceOfAnUnknownSource(t, source, identifier,
		func(s *bytesAudioStore) ports.StoredAudioReader { return storedBytesReader{store: s} })
	if err != nil {
		t.Fatalf("ExecuteReplace: %v, want the same-length upload with another fingerprint stored", err)
	}
	if !slices.Contains(source.fetched, sameLengthCurrentURL) || !slices.Contains(source.fetched, sameLengthOtherURL) {
		t.Errorf("fetched %v, want both same-length candidates tried", source.fetched)
	}
	if storesBytes(store, "the current file again") || !storesBytes(store, "another upload") {
		t.Errorf("store = %v, want only the upload with a different fingerprint", store.objects)
	}
}

func TestExecuteReplace_NoFingerprintExcludesNothing(t *testing.T) {
	storedBytes := func(s *bytesAudioStore) ports.StoredAudioReader { return storedBytesReader{store: s} }
	tests := map[string]struct {
		readerFor  func(*bytesAudioStore) ports.StoredAudioReader
		wantLogged string
	}{
		"the stored copy fails": {
			readerFor: func(s *bytesAudioStore) ports.StoredAudioReader {
				return storedBytesReader{store: s, err: errors.New("object storage unavailable")}
			},
			wantLogged: "the stored file could not be fingerprinted",
		},
		"AcoustID has no id for the stored file": {
			readerFor:  storedBytes,
			wantLogged: "AcoustID returned no id for the stored file",
		},
		"no stored-audio reader is wired": {
			wantLogged: "no stored-audio reader or identifier is wired",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			source := sameLengthUploads()
			identifier := acoustIDByBytesIdentifier{byBytes: map[string]ports.RecordingMatch{
				"the current file again": {AcoustID: "acoustid-of-the-stored-file", MBIDs: []string{recordingMBID}},
				"another upload":         {AcoustID: "acoustid-of-another-upload", MBIDs: []string{recordingMBID}},
			}}
			logs := captureServiceLogs(t)

			store, err := replaceOfAnUnknownSource(t, source, identifier, tt.readerFor)
			if err != nil {
				t.Fatalf("ExecuteReplace: %v, want the replace to carry on", err)
			}
			if !slices.Contains(source.fetched, sameLengthCurrentURL) {
				t.Errorf("fetched %v, want the same-length top candidate tried, not excluded", source.fetched)
			}
			if !storesBytes(store, "the current file again") {
				t.Errorf("store = %v, want the top candidate stored: nothing is excluded without a fingerprint", store.objects)
			}
			if !strings.Contains(logs.String(), "level=WARN msg=acquisition.replace_current_file_unidentified") ||
				!strings.Contains(logs.String(), tt.wantLogged) {
				t.Errorf("logs = %q, want the missing exclusion warned: %s", logs.String(), tt.wantLogged)
			}
		})
	}
}

func TestExecuteReplace_AnOutageWithOnlyTheCurrentFileLeftIsSourceUnavailableAndRecordsNothing(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	seedUnknownSourceTrack(t, ledger, userId, trackId)
	healthy := &uploadsByBytesSource{
		uploads: map[string]string{sameLengthCurrentURL: "the current file again"},
		found: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: sameLengthCurrentURL, Channel: "The Weeknd - Topic", Categories: []string{"Music"}, Duration: 112},
		},
	}
	rejections := &fakeRejectionStore{}
	identifier := acoustIDByBytesIdentifier{byBytes: map[string]ports.RecordingMatch{
		"original bytes":         {AcoustID: "acoustid-of-the-stored-file", MBIDs: []string{recordingMBID}},
		"the current file again": {AcoustID: "acoustid-of-the-stored-file", MBIDs: []string{recordingMBID}},
	}}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(outageSource(), healthy), store,
		withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: recordingMBID, Duration: 112}}),
		withServiceIdentifier(identifier), withServiceStoredAudio(storedBytesReader{store: store}), withServiceRejections(rejections))

	err := svc.ExecuteReplace(context.Background(), userId, trackId)

	if service.FailureCodeOf(err) != string(downloadfailure.FailureSourceUnavailable) {
		t.Errorf("failure code = %q (%v), want source_unavailable: a source was down and the only candidate left was the current file", service.FailureCodeOf(err), err)
	}
	if len(rejections.recorded) != 0 {
		t.Errorf("recorded = %+v, want nothing: the current file is not a lasting rejection", rejections.recorded)
	}
	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("stored audio = %q, want the original kept", got)
	}
}

func TestExecuteReplace_TheCurrentFileIsFoundWithoutAResolvedRecording(t *testing.T) {
	ledger, store, userId, trackId, _ := seedReadyTrackWith(t)
	seedUnknownSourceTrack(t, ledger, userId, trackId)
	source := sameLengthUploads()
	identifier := acoustIDByBytesIdentifier{byBytes: map[string]ports.RecordingMatch{
		"original bytes":         {AcoustID: "acoustid-of-the-stored-file"},
		"the current file again": {AcoustID: "acoustid-of-the-stored-file"},
		"another upload":         {AcoustID: "acoustid-of-another-upload"},
	}}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(source), store,
		withServiceResolver(&stubResolver{}), withServiceIdentifier(identifier), withServiceStoredAudio(storedBytesReader{store: store}))

	err := svc.ExecuteReplace(context.Background(), userId, trackId)
	if err != nil {
		t.Fatalf("ExecuteReplace: %v, want the other upload stored", err)
	}
	if storesBytes(store, "the current file again") || !storesBytes(store, "another upload") {
		t.Errorf("store = %v, want only the upload with a different fingerprint", store.objects)
	}
}

const (
	prodOverRejectedUpload = "https://www.youtube.com/watch?v=FjkkqbEbFOY"
	prodOverCurrentSource  = "https://music.youtube.com/watch?v=NxTPVbk-FZQ"
)

func prodOverTrack(t *testing.T, ledger *acquisitiontest.Ledger, userId ids.UserId, settle func(*ledgerTrack)) ledgerTrack {
	t.Helper()
	track := newLedgerTrack(userId, "Over", "LUCKI", "Watch My Back")
	settle(&track)
	track.RejectedSourceKeys = []string{"youtube:FjkkqbEbFOY"}
	ledger.Seed(track)
	return track
}

func prodOverCandidates() []ports.AudioCandidate {
	return []ports.AudioCandidate{
		{Title: "Lucki Ecks -  Over", URL: prodOverRejectedUpload, Channel: "DatPiff", Duration: 138, Kind: ports.KindUpload, Source: "fake"},
		{Title: "Over", URL: prodOverCurrentSource, Channel: "LUCKI", Duration: 136, Source: "fake"},
	}
}

func TestExecute_ExcludesTheUsersRejectedSources(t *testing.T) {
	missingFile := readyWith("lucki/watch-my-back/over.mp3")
	tests := []struct {
		name   string
		settle func(*ledgerTrack)
	}{
		{"first acquisition", func(*ledgerTrack) {}},
		{"retry after a failure", func(track *ledgerTrack) { makeFailed(track, "no match") }},
		{"reacquire of a missing file", missingFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := ids.NewUserId(uuid.New())
			ledger := acquisitiontest.NewLedger()
			track := prodOverTrack(t, ledger, userId, tt.settle)
			searcher := &fakeAudioSearcher{searchResults: prodOverCandidates(), downloadErr: errors.New("boom")}
			svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != prodOverCurrentSource {
				t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, prodOverCurrentSource)
			}
		})
	}
}

func TestExecute_FailsRatherThanDownloadTheOnlyCandidateTheUserRejected(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := prodOverTrack(t, ledger, userId, func(*ledgerTrack) {})
	searcher := &fakeAudioSearcher{searchResults: prodOverCandidates()[:1], downloadErr: errors.New("boom")}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 0 {
		t.Errorf("download attempts = %v, want none: the only candidate is the one the user rejected", searcher.downloadURLs)
	}
}

func TestExecuteReplace_StillExcludesTheUsersRejectedSourcesAndTheCurrentOne(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := prodOverTrack(t, ledger, userId, func(track *ledgerTrack) {
		makeReady(track, "lucki/watch-my-back/over.mp3")
		useSource(track, prodOverCurrentSource)
	})
	store := newFakeAudioStore()
	store.stored["lucki/watch-my-back/over.mp3"] = true
	other := ports.AudioCandidate{Title: "Over [prod. Ugly Friend]", URL: "https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A321193537", Channel: "LUCKI", Duration: 136, Kind: ports.KindUpload, Source: "fake"}
	searcher := &fakeAudioSearcher{searchResults: append(prodOverCandidates(), other), downloadErr: errors.New("boom")}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != other.URL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, other.URL)
	}
}

type searcherDownloads struct{ searcher *fakeAudioSearcher }

func (searcherDownloads) MarkUnplayable(context.Context, []ports.AudioCandidate) {}

func (d searcherDownloads) Download(ctx context.Context, url, outDir string) (ports.FetchedAudio, error) {
	return fetchedFrom(d.searcher.Download(ctx, url, outDir))
}

func TestExecute_AMissingFileTriesItsStoredSourceFirst(t *testing.T) {
	const storedSource = "https://music.youtube.com/watch?v=NxTPVbk-FZQ"
	rivals := []string{
		"https://music.youtube.com/watch?v=aaaaaaaaaaa",
		"https://music.youtube.com/watch?v=aaaaaaaaaab",
		"https://music.youtube.com/watch?v=aaaaaaaaaac",
	}
	tests := []struct {
		name     string
		rejected []string
		inWindow bool
	}{
		{"the stored source goes in the first window", nil, true},
		{"a rejected stored source is never tried", []string{sourcekey.SourceKey(storedSource)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := ids.NewUserId(uuid.New())
			ledger := acquisitiontest.NewLedger()
			track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", storedSource)
			track.RejectedSourceKeys = tt.rejected
			ledger.Seed(track)
			var found []ports.AudioCandidate
			for _, u := range rivals {
				found = append(found, ports.AudioCandidate{Title: "Blinding Lights", URL: u, Channel: "The Weeknd", Artists: []string{"The Weeknd"}, Kind: ports.KindArtTrack, Source: "fake"})
			}
			searcher := &fakeAudioSearcher{searchResults: found, downloadErr: errors.New("boom")}
			registry := service.NewSourceRegistry(savedlink.NewSource(searcherDownloads{searcher}), searcher)
			svc := newTestAcquireService(ledger, registry, newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			firstWindow := searcher.downloadURLs[:min(2, len(searcher.downloadURLs))]
			if got := slices.Contains(firstWindow, storedSource); got != tt.inWindow {
				t.Errorf("first window = %v, stored source in it = %v, want %v", firstWindow, got, tt.inWindow)
			}
			if !tt.inWindow && slices.Contains(searcher.downloadURLs, storedSource) {
				t.Errorf("download attempts = %v, want the rejected stored source never tried", searcher.downloadURLs)
			}
		})
	}
}

func TestExecuteReplace_NeverOffersTheCurrentSourceAsTheKnownOne(t *testing.T) {
	const storedSource = "https://music.youtube.com/watch?v=NxTPVbk-FZQ"
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", storedSource)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	rival := ports.AudioCandidate{Title: "Blinding Lights", URL: "https://music.youtube.com/watch?v=aaaaaaaaaaa", Kind: ports.KindArtTrack, Artists: []string{"The Weeknd"}, Source: "fake"}
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{rival}, downloadErr: errors.New("boom")}
	registry := service.NewSourceRegistry(savedlink.NewSource(searcherDownloads{searcher}), searcher)
	svc := newTestAcquireService(ledger, registry, store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != rival.URL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, rival.URL)
	}
}

func TestExecuteReplace_NeverFetchesTheCurrentSourceAgainEvenWhenItRanksFirst(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	wrongURL := "https://youtube.com/watch?v=wrongwrong1"
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", wrongURL)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{wrongURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q, never the current source %q", searcher.downloadURLs, otherURL, wrongURL)
	}
}

type lowConfidenceSource struct{ unknownTopicSource }

func (lowConfidenceSource) Name() acqdomain.SourceName { return "low-confidence" }

func (lowConfidenceSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:    "Blinding Lights",
		URL:      "https://www.youtube.com/watch?v=reuploadAAA",
		Channel:  "chill uploads",
		Duration: 220,
	}}, nil
}

func acquireLowConfidence(t *testing.T, opts ...any) (ledgerTrack, error) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	seconds := 200.0
	track.StoredDurationSeconds = &seconds
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(lowConfidenceSource{}),
		&bytesAudioStore{objects: map[string]string{}},
		append([]any{
			withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding"}}),
			withServiceIdentifier(&stubIdentifier{}),
		}, opts...)...)
	err := svc.Execute(context.Background(), userId, track.ID)
	return storedTrack(t, ledger, track), err
}

func TestExecute_AConfiguredZeroFloorIsHonoured(t *testing.T) {
	track, err := acquireLowConfidence(t, service.WithAcquireConfidenceFloor(0))
	if err != nil {
		t.Fatalf("a floor of 0 should store the held candidate, got %v", err)
	}
	if track.Status != ports.TrackReady {
		t.Errorf("status = %v, want ready", track.Status)
	}

	_, err = acquireLowConfidence(t)
	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Errorf("with no option err = %v, want ErrNoConfidentMatch at the 0.5 default", err)
	}
}

func retryWithBasis(t *testing.T, catalogLength float64, identity ports.RecordingIdentity, prior []ports.PriorRejection, urls ...string) *fakeAudioSearcher {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := queuedTrack(t, ledger, userId)
	if catalogLength > 0 {
		track = withCatalogLength(t, ledger, track, catalogLength)
	}
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore(),
		withServiceRejections(&fakeRejectionStore{active: prior}), withServiceResolver(&stubResolver{identity: identity}))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher
}

func TestExecute_APriorRejectionJudgedOnTheResolvedLengthIsSkippedWhenTheTrackHasNoCatalogLength(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: runMBID})

	searcher := retryWithBasis(t, 0, ports.RecordingIdentity{MBID: runMBID, Duration: 200}, prior, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q: the run resolved 200s and %s, the same basis the rejection was judged on", searcher.downloadURLs, otherURL, runMBID)
	}
}

func TestExecute_APriorRejectionJudgedWithNoLengthIsRetriedOnceTheCatalogLengthIsKnown(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 0, ReferenceMBID: runMBID})

	searcher := retryWithBasis(t, 200, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: it was judged with no expected length, the run now expects 200s", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_APriorRejectionJudgedOnALengthIsRetriedWhenTheRunHasNoLength(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: runMBID})

	searcher := retryWithBasis(t, 0, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: it was judged on 200s, the run knows no length", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_APriorRejectionJudgedWithNoMBIDIsRetriedOnceTheMBIDIsKnown(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: ""})

	searcher := retryWithBasis(t, 200, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: it was judged with no reference MBID, the run now has %s", searcher.downloadURLs, drmURL, runMBID)
	}
}

func TestExecute_APriorRejectionJudgedOnAnMBIDIsRetriedWhenTheRunResolvesNone(t *testing.T) {
	prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: runMBID})

	searcher := retryWithBasis(t, 200, ports.RecordingIdentity{}, prior, drmURL, otherURL)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: it was judged against %s, the run resolved no MBID", searcher.downloadURLs, drmURL, runMBID)
	}
}

func TestExecute_APriorRejectionAppliesOnlyWhileTheLengthIsWithinTheAuthoritativeWindow(t *testing.T) {
	cases := []struct {
		name     string
		recorded float64
		skipped  bool
	}{
		{"just inside above", 205.5, true},
		{"just inside below", 194.5, true},
		{"just outside above", 206.5, false},
		{"just outside below", 193.5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prior := priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: tc.recorded, ReferenceMBID: runMBID})

			searcher := retryWithBasis(t, 200, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

			if got := slices.Contains(searcher.downloadURLs, drmURL); got == tc.skipped {
				t.Errorf("recorded %vs vs run 200s: %q downloaded = %v, want %v (attempts %v)", tc.recorded, drmURL, got, !tc.skipped, searcher.downloadURLs)
			}
		})
	}
}

func TestExecute_AStaleRejectionDoesNotReviveASourceRejectedOnTheSameBasis(t *testing.T) {
	prior := []ports.PriorRejection{
		{SourceKey: sourcekey.SourceKey(drmURL), Basis: &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: runMBID}},
		{SourceKey: sourcekey.SourceKey(otherURL), Basis: &ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: runMBID}},
	}

	searcher := retryWithBasis(t, 200, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q: its rejection is stale so not every candidate was skipped", searcher.downloadURLs, otherURL)
	}
}

func TestExecuteReplace_RetriesAPriorRejectionJudgedOnAnotherLength(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")
	track = withCatalogLength(t, ledger, track, 200)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{drmURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	rejections := &fakeRejectionStore{active: priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: runMBID})}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store,
		withServiceRejections(rejections), withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: its rejection was judged on 150s, the replace run expects 200s", searcher.downloadURLs, drmURL)
	}
}

type stalledSource struct{}

func (stalledSource) Name() acqdomain.SourceName { return "stalled" }

func (stalledSource) Find(ctx context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSource) Fetch(ctx context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	<-ctx.Done()
	return ports.FetchedAudio{}, ctx.Err()
}

func executeAfterOwnTimeBudget(t *testing.T, attempt int) (ledgerTrack, *recordingPublisher, error) {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(stalledSource{}), newFakeAudioStore(), withServiceEvents(pub), service.WithAcquireTimeout(time.Millisecond))
	ctx := service.WithJobRun(context.Background(), service.JobRun{Attempt: attempt, SchedulerOwned: true})

	execErr := svc.Execute(ctx, userId, track.ID)

	return storedTrack(t, ledger, track), pub, execErr
}

func TestExecute_AnExpiredOwnTimeBudgetIsRetried(t *testing.T) {
	stored, pub, execErr := executeAfterOwnTimeBudget(t, 1)
	if pub == nil {
		t.Fatal("no publisher")
	}

	if !errors.Is(execErr, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want ErrAcquisitionRetryable", execErr)
	}
	if !errors.Is(execErr, service.ErrAcquisitionTimedOut) {
		t.Errorf("err = %v, want ErrAcquisitionTimedOut", execErr)
	}
	if stored.Status != ports.TrackPending {
		t.Errorf("status = %v, want pending", stored.Status)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("failed publishes = %d, want 0", got)
	}
}

func TestExecute_AnExpiredOwnTimeBudgetOnTheLastAttemptIsSourceUnavailable(t *testing.T) {
	stored, pub, execErr := executeAfterOwnTimeBudget(t, service.MaxAcquisitionAttempts)
	if pub == nil {
		t.Fatal("no publisher")
	}

	if errors.Is(execErr, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", execErr)
	}
	if stored.Status != ports.TrackFailed {
		t.Fatalf("status = %v, want failed", stored.Status)
	}
	if got := stored.FailureReason; got != string(downloadfailure.FailureSourceUnavailable) {
		t.Errorf("failure reason = %q, want %q", got, downloadfailure.FailureSourceUnavailable)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 1 {
		t.Errorf("failed publishes = %d, want 1", got)
	}
}

type replaceFixture struct {
	svc    *service.AcquireTrackAudioService
	ledger *acquisitiontest.Ledger
	track  ledgerTrack
	store  *fakeAudioStore
	pub    *recordingPublisher
	userId ids.UserId
	ctx    context.Context
}

func newReplaceFixture(t *testing.T, searcher *fakeAudioSearcher, attempt int) replaceFixture {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	track := readyTrackWithSource(t, ledger, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	pub := newRecordingPublisher()
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), store, withServiceEvents(pub))
	ctx := service.WithJobRun(context.Background(), service.JobRun{Attempt: attempt, SchedulerOwned: true})
	return replaceFixture{svc: svc, ledger: ledger, track: track, store: store, pub: pub, userId: userId, ctx: ctx}
}

func TestExecuteReplace_ATransientOutageBelowTheCapIsRetried(t *testing.T) {
	for attempt := 1; attempt < service.MaxAcquisitionAttempts; attempt++ {
		f := newReplaceFixture(t, transientSearchFailure(), attempt)
		err := f.svc.ExecuteReplace(f.ctx, f.userId, f.track.ID)
		stored, store, pub := storedTrack(t, f.ledger, f.track), f.store, f.pub

		if !errors.Is(err, service.ErrAcquisitionRetryable) {
			t.Errorf("attempt %d: err = %v, want ErrAcquisitionRetryable", attempt, err)
		}
		if stored.Status != ports.TrackReady || stored.AudioRef == nil || *stored.AudioRef != "u/a/b/c.mp3" {
			t.Errorf("attempt %d: status = %v, audio_ref = %v, want the track ready with its audio", attempt, stored.Status, stored.AudioRef)
		}
		if !store.stored["u/a/b/c.mp3"] {
			t.Errorf("attempt %d: the user's audio must survive", attempt)
		}
		if got := pub.count(events.TypeTrackReplaceFailed); got != 0 {
			t.Errorf("attempt %d: replace-failed publishes = %d, want 0", attempt, got)
		}
	}
}

func TestExecuteReplace_ATransientOutageOnTheLastAttemptFails(t *testing.T) {
	f := newReplaceFixture(t, transientSearchFailure(), service.MaxAcquisitionAttempts)
	err := f.svc.ExecuteReplace(f.ctx, f.userId, f.track.ID)
	stored, pub := storedTrack(t, f.ledger, f.track), f.pub

	if err == nil || errors.Is(err, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", err)
	}
	if stored.Status != ports.TrackReady {
		t.Errorf("status = %v, want the track left ready", stored.Status)
	}
	if got := pub.count(events.TypeTrackReplaceFailed); got != 1 {
		t.Errorf("replace-failed publishes = %d, want 1", got)
	}
}

func TestReplaceGated_ATransientOutageBelowTheCapIsNotRetried(t *testing.T) {
	f := newReplaceFixture(t, transientSearchFailure(), 1)
	snapshot, err := f.ledger.Get(f.ctx, f.track.ID, f.userId)
	if err != nil || snapshot == nil {
		t.Fatalf("load track: snapshot = %v, err = %v", snapshot, err)
	}
	_, err = f.svc.ReplaceGated(f.ctx, f.userId, snapshot, replacement.ReplacementGate{})

	if err == nil || errors.Is(err, service.ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure: the upgrade lane keeps its own retry policy", err)
	}
}

func TestExecute_AWorkerThatLostItsLeaseDoesNotFailTheTrack(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 2
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Claim: 1}), userId, track.ID)

	if got := committedTrack(t, ledger, track.ID).Status; got != ports.TrackPending {
		t.Errorf("status = %v, want pending: the new lease owner is still downloading", got)
	}
}

func TestExecute_AWorkerHoldingTheCurrentLeaseStillFailsTheTrack(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "Song", "Artist", "Album")
	track.ClaimNumber = 2
	ledger := acquisitiontest.NewLedger()
	ledger.Seed(track)
	svc := newTestAcquireService(ledger, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Claim: 2}), userId, track.ID)

	if got := committedTrack(t, ledger, track.ID).Status; got != ports.TrackFailed {
		t.Errorf("status = %v, want failed", got)
	}
}

func TestExecute_AWorkerThatLostItsLeaseCannotLandItsDownload(t *testing.T) {
	cases := []struct {
		name       string
		held       ports.ClaimNumber
		wantStatus ports.TrackStatus
		wantAudio  bool
	}{
		{"the lease was reclaimed", 1, ports.TrackPending, false},
		{"the lease is still held", 2, ports.TrackReady, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := ids.NewUserId(uuid.New())
			track := newLedgerTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
			track.ClaimNumber = 2
			ledger := acquisitiontest.NewLedger(track)
			store := &bytesAudioStore{objects: map[string]string{}}
			svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

			_ = svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Claim: tc.held}), userId, track.ID)

			row := committedTrack(t, ledger, track.ID)
			if row.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v", row.Status, tc.wantStatus)
			}
			if (row.AudioRef != nil) != tc.wantAudio {
				t.Errorf("audio_ref = %v, want set = %v", row.AudioRef, tc.wantAudio)
			}
			if row.FailureReason != "" {
				t.Errorf("failure_reason = %q, want none", row.FailureReason)
			}
		})
	}
}

func TestExecuteReplace_AWorkerThatLostItsLeaseLeavesTheServingAudioInPlace(t *testing.T) {
	ledger, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	row := committedTrack(t, ledger, trackId)
	row.ClaimNumber = 2
	ledger.Seed(row)
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(newAudioSource{}), store)

	_ = svc.ExecuteReplace(service.WithJobRun(context.Background(), service.JobRun{Claim: 1}), userId, trackId)

	got := committedTrack(t, ledger, trackId)
	if got.Status != ports.TrackReady || got.AudioRef == nil || *got.AudioRef != originalRef {
		t.Errorf("track = %v %v, want ready on the original ref %q", got.Status, got.AudioRef, originalRef)
	}
	if store.objects[originalRef] != "original bytes" {
		t.Errorf("object at the serving ref = %q, want the original audio kept", store.objects[originalRef])
	}
}

func TestExecute_TheSourceIsAskedWithTheCatalogFacts(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	ledger := acquisitiontest.NewLedger()
	explicit := true
	sources := []ports.CatalogSource{
		{Provider: "spotify", ExternalID: "4uLU6hMCjMI75M1A2tKUQC", URL: "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC"},
		{Provider: "deezer", ExternalID: "3135556"},
	}
	track := seedTrackInRepo(t, ledger, userId, func(tr *ledgerTrack) {
		tr.ISRC = ptrTo("USUM71703861")
		tr.CatalogDurationSeconds = ptrTo(214.0)
		tr.StoredDurationSeconds = ptrTo(200.0)
		tr.CatalogExplicit, tr.CatalogSources = &explicit, sources
	})
	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(ledger, fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(searcher.requests) != 1 {
		t.Fatalf("source asked %d times, want 1", len(searcher.requests))
	}
	req := searcher.requests[0]
	if req.Title != "Song" || req.Artist != "Artist" || req.Album != "Album" {
		t.Errorf("request title/artist/album = %q/%q/%q, want Song/Artist/Album", req.Title, req.Artist, req.Album)
	}
	if req.ISRC != "USUM71703861" {
		t.Errorf("request ISRC = %q, want USUM71703861", req.ISRC)
	}
	if req.Duration != 214 {
		t.Errorf("request Duration = %v, want the catalog length 214 (stored length is 200)", req.Duration)
	}
	want := []ports.CatalogSource{
		{Provider: "spotify", ExternalID: "4uLU6hMCjMI75M1A2tKUQC", URL: "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC"},
		{Provider: "deezer", ExternalID: "3135556"},
	}
	if !slices.Equal(req.CatalogSources, want) {
		t.Errorf("request CatalogSources = %+v, want %+v", req.CatalogSources, want)
	}
	if req.CatalogExplicit == nil || !*req.CatalogExplicit {
		t.Errorf("request CatalogExplicit = %v, want true", req.CatalogExplicit)
	}
}

type completionRecordingLedger struct {
	ports.LibraryTracks
	completed []ports.Completion
}

func (l *completionRecordingLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence ports.ClaimNumber, c ports.Completion) error {
	l.completed = append(l.completed, c)
	return l.LibraryTracks.Complete(ctx, id, userId, fence, c)
}

const lowBitrateStoredSource = "https://www.youtube.com/watch?v=currentAAAA"

type audioOnlyOfTheStoredSource struct{ newAudioSource }

func (audioOnlyOfTheStoredSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        lowBitrateStoredSource,
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
		Kind:       ports.KindArtTrack,
		Duration:   200,
	}}, nil
}

func (s audioOnlyOfTheStoredSource) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	fetched, err := s.newAudioSource.Fetch(ctx, c, outDir)
	fetched.Format = ports.SourceFormat{ID: "140", Codec: "mp4a.40.2", Kbps: 130}
	return fetched, err
}

func TestReplaceGated_AMuxedFallbackIsSwappedForTheAudioOnlyStreamOfTheSameSource(t *testing.T) {
	book, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	ledger := &completionRecordingLedger{LibraryTracks: book}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(audioOnlyOfTheStoredSource{}), store,
		withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
		withServiceIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
	)
	track, err := ledger.Get(context.Background(), trackId, userId)
	if err != nil || track == nil {
		t.Fatalf("load track: %v %v", track, err)
	}
	gate := replacement.ReplacementGate{
		Current:   audioquality.A,
		Stored:    ports.SourceFormat{ID: "18", Codec: "mp4a.40.2", Kbps: 96},
		FloorKbps: 112,
	}

	if _, err := svc.ReplaceGated(context.Background(), userId, track, gate); err != nil {
		t.Fatalf("ReplaceGated: %v, want the 140 stream at 130 kbps admitted over the stored format 18 at 96", err)
	}

	if row := committedTrack(t, book, trackId); row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Fatalf("audio_ref = %v, want the swap committed to a new ref", row.AudioRef)
	}
	if len(ledger.completed) != 1 {
		t.Fatalf("completions = %d, want 1", len(ledger.completed))
	}
	done := ledger.completed[0]
	var evidence struct {
		SourceFormatID string `json:"source_format_id"`
		SourceKbps     int    `json:"source_kbps"`
	}
	if err := json.Unmarshal(done.Evidence, &evidence); err != nil {
		t.Fatalf("evidence %s: %v", done.Evidence, err)
	}
	if evidence.SourceFormatID != "140" || evidence.SourceKbps != 130 {
		t.Errorf("evidence = %+v, want the fetched format 140 at 130 kbps", evidence)
	}
	if slices.Contains(done.RejectedSourceKeys, sourcekey.SourceKey(lowBitrateStoredSource)) {
		t.Errorf("rejected keys = %v, want the stored source left out: the upgrade refetches it", done.RejectedSourceKeys)
	}
}

type fullLengthAlbumCut struct{ newAudioSource }

func (fullLengthAlbumCut) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	found, err := newAudioSource{}.Find(ctx, req)
	for i := range found {
		found[i].Duration = 285
	}
	return found, err
}

func TestReplaceGated_AFileThatFitsTheCatalogLengthReplacesAStoredEditAtTheSameGrade(t *testing.T) {
	book, store, userId, trackId, originalRef := seedReadyTrackWith(t)
	ledger := &completionRecordingLedger{LibraryTracks: book}
	svc := newTestAcquireService(ledger, service.NewSourceRegistry(fullLengthAlbumCut{}), store,
		withServiceProber(&queueProber{durations: []float64{285, 285, 285}}),
		withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
		withServiceIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
	)
	track, err := ledger.Get(context.Background(), trackId, userId)
	if err != nil || track == nil {
		t.Fatalf("load track: %v %v", track, err)
	}
	catalog := 285.0
	track.CatalogDurationSeconds = &catalog
	gate := replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}

	if _, err := svc.ReplaceGated(context.Background(), userId, track, gate); err != nil {
		t.Fatalf("ReplaceGated: %v, want the 285 s file admitted over the 231 s edit", err)
	}

	if row := committedTrack(t, book, trackId); row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Fatalf("audio_ref = %v, want the swap committed to a new ref", row.AudioRef)
	}
	if len(ledger.completed) != 1 || ledger.completed[0].Duration.Seconds() != 285 {
		t.Errorf("completions = %+v, want one carrying the 285 s length", ledger.completed)
	}
}

func TestReplaceGated_ASameGradeFileIsRefusedWhenTheLengthRescueDoesNotHold(t *testing.T) {
	cases := []struct {
		name string
		gate replacement.ReplacementGate
	}{
		{"the stored file already fits the catalog length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 284}},
		{"the fetched 285 s file misses a 195 s catalog length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 195, StoredSeconds: 292}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			book, store, userId, trackId, originalRef := seedReadyTrackWith(t)
			ledger := &completionRecordingLedger{LibraryTracks: book}
			svc := newTestAcquireService(ledger, service.NewSourceRegistry(fullLengthAlbumCut{}), store,
				withServiceResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
				withServiceIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
				withServiceProber(&queueProber{durations: []float64{285, 285, 285}}),
			)
			track, err := ledger.Get(context.Background(), trackId, userId)
			if err != nil || track == nil {
				t.Fatalf("load track: %v %v", track, err)
			}

			if _, err := svc.ReplaceGated(context.Background(), userId, track, tc.gate); !errors.Is(err, replacement.ErrNotAnUpgrade) {
				t.Fatalf("ReplaceGated = %v, want ErrNotAnUpgrade for the same-grade 285 s file", err)
			}

			if row := committedTrack(t, book, trackId); row.AudioRef == nil || *row.AudioRef != originalRef {
				t.Errorf("audio_ref = %v, want the original %q kept", row.AudioRef, originalRef)
			}
			if len(ledger.completed) != 0 {
				t.Errorf("completions = %+v, want none", ledger.completed)
			}
		})
	}
}

const (
	recklessMBID        = "7933a65c-2c66-4ec1-9880-e46b5d2f05de"
	classicVersionURL   = "https://music.youtube.com/watch?v=Ml2N72dMv_8"
	recklessReuploadURL = "https://www.youtube.com/watch?v=reuploadIOL"
	recklessTopicURL    = "https://www.youtube.com/watch?v=recklessIOL"
)

func goldenRerecording() ports.TrackLabel {
	return ports.TrackLabel{
		MBID:            "25d68187-ee71-4f06-9e58-0b49d66bf8a8",
		Title:           "It’s Only Love (classic version)",
		Album:           "It’s Only Love",
		Year:            2025,
		ISRC:            "IETLH2500023",
		DurationSeconds: 194,
	}
}

type stubRerecordingFinder struct {
	label   ports.TrackLabel
	found   bool
	err     error
	queries []ports.RerecordingQuery
}

func (f *stubRerecordingFinder) FindRerecording(_ context.Context, q ports.RerecordingQuery) (ports.TrackLabel, bool, error) {
	f.queries = append(f.queries, q)
	return f.label, f.found, f.err
}

func findsGoldenRerecording() *stubRerecordingFinder {
	return &stubRerecordingFinder{label: goldenRerecording(), found: true}
}

type pinnedRecordingResolver struct{}

func (pinnedRecordingResolver) Resolve(_ context.Context, q ports.RecordingQuery) (ports.RecordingIdentity, error) {
	return ports.RecordingIdentity{MBID: q.MBID, MBIDs: []string{q.MBID}}, nil
}

type itsOnlyLoveSources struct {
	originalOnTopic    bool
	originalOutage     bool
	rerecordingBroken  bool
	originalFetchFails bool
	requests           []ports.FindRequest
}

func (*itsOnlyLoveSources) Name() acqdomain.SourceName { return "its-only-love" }

func (s *itsOnlyLoveSources) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.requests = append(s.requests, req)
	switch {
	case req.Title == goldenRerecording().Title:
		return []ports.AudioCandidate{{Title: req.Title, URL: classicVersionURL, Channel: "Bryan Adams - Topic", Categories: []string{"Music"}, Duration: 194}}, nil
	case s.originalOutage:
		return nil, &acqdomain.SourceUnavailableError{Source: "its-only-love", Err: errors.New("http error 503")}
	case s.originalOnTopic:
		return []ports.AudioCandidate{{Title: "It's Only Love", URL: recklessTopicURL, Channel: "Bryan Adams - Topic", Categories: []string{"Music"}, Duration: 195}}, nil
	default:
		return []ports.AudioCandidate{
			{Title: "It's Only Love", URL: recklessReuploadURL, Channel: "chill uploads", Duration: 215},
			{Title: "It's Only Love", URL: recklessDRMURL, Channel: "Bryan Adams - Topic", Categories: []string{"Music"}, Duration: 195, Unplayable: ports.UnplayableDRM},
		}, nil
	}
}

func (s *itsOnlyLoveSources) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if c.URL == recklessTopicURL && s.originalFetchFails {
		return ports.FetchedAudio{}, errors.New("fetch: http error 403")
	}
	if c.URL == classicVersionURL && s.rerecordingBroken {
		return ports.FetchedAudio{}, errors.New("fetch: http error 403")
	}
	path := filepath.Join(outDir, "audio.m4a")
	fetched, err := fetchedFrom(path, os.WriteFile(path, []byte("audio of "+c.URL), 0o600))
	fetched.Format = ports.SourceFormat{ID: "140", Codec: "mp4a.40.2", Kbps: 128}
	return fetched, err
}

type itsOnlyLoveTrack struct {
	book    *acquisitiontest.Ledger
	store   *bytesAudioStore
	ledger  *completionRecordingLedger
	userId  ids.UserId
	trackId ids.TrackId
	ref     string
}

func seedItsOnlyLove(t *testing.T, storedSeconds float64, sources []ports.CatalogSource) itsOnlyLoveTrack {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	track := newLedgerTrack(userId, "It's Only Love", "Bryan Adams", "Reckless")
	isrc, mbid := "USAM18490008", recklessMBID
	track.ISRC = &isrc
	track.CatalogDurationSeconds = ptrTo(195.0)
	track.CatalogMBID, track.CatalogSources = &mbid, sources
	store := &bytesAudioStore{objects: map[string]string{}}
	var ref string
	if storedSeconds > 0 {
		ref = service.BuildAudioRef(trackRefOf(track), "audio.m4a")
		makeReady(&track, ref)
		useSource(&track, "https://www.youtube.com/watch?v=storedIOLAA")
		track.StoredDurationSeconds = &storedSeconds
		store.objects[ref] = "stored bytes"
	}
	ledger := acquisitiontest.NewLedger(track)
	return itsOnlyLoveTrack{book: ledger, store: store, ledger: &completionRecordingLedger{LibraryTracks: ledger}, userId: userId, trackId: track.ID, ref: ref}
}

func (f itsOnlyLoveTrack) service(sources *itsOnlyLoveSources, finder ports.RerecordingFinder, extra ...any) *service.AcquireTrackAudioService {
	return newTestAcquireService(f.ledger, service.NewSourceRegistry(sources), f.store,
		append([]any{
			withServiceResolver(pinnedRecordingResolver{}),
			withServiceIdentifier(&stubIdentifier{}),
			withServiceRerecordings(finder),
		}, extra...)...)
}

func (f itsOnlyLoveTrack) row() ledgerTrack {
	row, ok := f.book.Track(f.trackId)
	if !ok {
		panic("the golden track is missing from the ledger")
	}
	return row
}

func TestRerecording_NoConfidentOriginalTakesTheOfficialRerecordingLabelledAsIt(t *testing.T) {
	cases := []struct {
		name          string
		storedSeconds float64
		run           func(*service.AcquireTrackAudioService, context.Context, ids.UserId, ids.TrackId) error
	}{
		{"replace of the 292 s live file", 292, (*service.AcquireTrackAudioService).ExecuteReplace},
		{"acquire", 0, (*service.AcquireTrackAudioService).Execute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := seedItsOnlyLove(t, tc.storedSeconds, nil)
			finder := findsGoldenRerecording()

			if err := tc.run(golden.service(&itsOnlyLoveSources{}, finder), context.Background(), golden.userId, golden.trackId); err != nil {
				t.Fatalf("run: %v, want the re-recording committed", err)
			}

			if len(finder.queries) != 1 || finder.queries[0].OriginalMBID != recklessMBID {
				t.Errorf("finder queries = %+v, want one for the Reckless recording", finder.queries)
			}
			row := golden.row()
			if row.AudioRef == nil || golden.store.objects[*row.AudioRef] != "audio of "+classicVersionURL {
				t.Fatalf("serving audio = %v, want the classic version's audio", row.AudioRef)
			}
			if len(golden.ledger.completed) != 1 || golden.ledger.completed[0].Relabel == nil || *golden.ledger.completed[0].Relabel != goldenRerecording() {
				t.Fatalf("completions = %+v, want one relabelled as the golden re-recording", golden.ledger.completed)
			}
			if row.Title != goldenRerecording().Title || row.CatalogMBID == nil || *row.CatalogMBID != goldenRerecording().MBID {
				t.Errorf("track = %q %v, want it labelled as the classic version", row.Title, row.CatalogMBID)
			}
		})
	}
}

func TestRerecording_AFoundOriginalNeverAsksForARerecording(t *testing.T) {
	golden := seedItsOnlyLove(t, 0, nil)
	finder := findsGoldenRerecording()

	if err := golden.service(&itsOnlyLoveSources{originalOnTopic: true}, finder).Execute(context.Background(), golden.userId, golden.trackId); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(finder.queries) != 0 {
		t.Errorf("finder queries = %+v, want none once the original is found", finder.queries)
	}
	if len(golden.ledger.completed) != 1 || golden.ledger.completed[0].Relabel != nil {
		t.Fatalf("completions = %+v, want one with no relabel", golden.ledger.completed)
	}
	if row := golden.row(); row.AudioRef == nil || golden.store.objects[*row.AudioRef] != "audio of "+recklessTopicURL {
		t.Errorf("serving audio = %v, want the Reckless recording", row.AudioRef)
	}
}

func TestRerecording_AFailedRerecordingTierKeepsTheOriginalFailure(t *testing.T) {
	cases := []struct {
		name      string
		sources   *itsOnlyLoveSources
		finder    *stubRerecordingFinder
		wantCode  downloadfailure.FailureCode
		wantAsked int
	}{
		{"the finder errors", &itsOnlyLoveSources{}, &stubRerecordingFinder{err: errors.New("musicbrainz 503")}, downloadfailure.FailureNoConfidentMatch, 1},
		{"the finder finds nothing", &itsOnlyLoveSources{}, &stubRerecordingFinder{}, downloadfailure.FailureNoConfidentMatch, 1},
		{"the re-recording pass fails too", &itsOnlyLoveSources{rerecordingBroken: true}, findsGoldenRerecording(), downloadfailure.FailureNoConfidentMatch, 1},
		{"the original is found but its download fails", &itsOnlyLoveSources{originalOnTopic: true, originalFetchFails: true}, findsGoldenRerecording(), downloadfailure.FailureDownloadFailed, 0},
		{"a source outage", &itsOnlyLoveSources{originalOutage: true}, findsGoldenRerecording(), downloadfailure.FailureSourceUnavailable, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := seedItsOnlyLove(t, 0, nil)

			err := golden.service(tc.sources, tc.finder).Execute(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

			if got := failureCodeOf(err); got != tc.wantCode {
				t.Errorf("failure code = %q (%v), want %q", got, err, tc.wantCode)
			}
			if len(tc.finder.queries) != tc.wantAsked {
				t.Errorf("finder asked %d times, want %d", len(tc.finder.queries), tc.wantAsked)
			}
			if len(golden.ledger.completed) != 0 {
				t.Errorf("completions = %+v, want none", golden.ledger.completed)
			}
			for _, req := range tc.sources.requests {
				if tc.wantAsked == 0 && req.Title == goldenRerecording().Title {
					t.Errorf("searched %q after an outage, want no re-recording pass", req.Title)
				}
			}
		})
	}
}

func TestRerecording_AReplaceOfAFittingOriginalTakesNoRerecording(t *testing.T) {
	golden := seedItsOnlyLove(t, 195, nil)
	finder := findsGoldenRerecording()

	err := golden.service(&itsOnlyLoveSources{}, finder).ExecuteReplace(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

	if got := failureCodeOf(err); got != downloadfailure.FailureNoConfidentMatch {
		t.Errorf("failure code = %q (%v), want no_confident_match", got, err)
	}
	if len(finder.queries) != 0 {
		t.Errorf("finder queries = %+v, want none for a stored file that fits the catalog length", finder.queries)
	}
	if row := golden.row(); row.AudioRef == nil || *row.AudioRef != golden.ref || row.Title != "It's Only Love" {
		t.Errorf("track = %q on %v, want the original kept on %q", row.Title, row.AudioRef, golden.ref)
	}
}

func TestRerecording_AReplaceOfAFileWhoseLengthIsUnknownTakesNoRerecording(t *testing.T) {
	cases := []struct {
		name   string
		forget func(row *ledgerTrack)
	}{
		{"no catalog length beside a stored file far off the old one", func(row *ledgerTrack) {
			stored := 292.0
			row.CatalogDurationSeconds, row.StoredDurationSeconds = nil, &stored
		}},
		{"no stored length beside the catalog length", func(row *ledgerTrack) { row.StoredDurationSeconds = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := seedItsOnlyLove(t, 195, nil)
			row := golden.row()
			tc.forget(&row)
			golden.book.Seed(row)
			finder := findsGoldenRerecording()

			err := golden.service(&itsOnlyLoveSources{}, finder).ExecuteReplace(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

			if got := failureCodeOf(err); got != downloadfailure.FailureNoConfidentMatch {
				t.Errorf("failure code = %q (%v), want no_confident_match from the search, not an earlier failure", got, err)
			}
			if len(finder.queries) != 0 {
				t.Errorf("finder queries = %+v, want none: an unknown length never shows the stored file is wrong", finder.queries)
			}
		})
	}
}

func TestRerecording_AGatedUpgradeNeverTakesARerecording(t *testing.T) {
	for _, storedSeconds := range []float64{195, 292} {
		golden := seedItsOnlyLove(t, storedSeconds, nil)
		finder := findsGoldenRerecording()
		sources := &itsOnlyLoveSources{}
		track, err := golden.ledger.Get(context.Background(), golden.trackId, golden.userId)
		if err != nil || track == nil {
			t.Fatalf("load track: %v %v", track, err)
		}

		_, err = golden.service(sources, finder).ReplaceGated(context.Background(), golden.userId, track, replacement.ReplacementGate{Current: audioquality.D, CatalogSeconds: 195, StoredSeconds: storedSeconds})

		if err == nil {
			t.Errorf("stored %v s: ReplaceGated succeeded, want the stored file kept", storedSeconds)
		}
		if len(finder.queries) != 0 || len(sources.requests) != 1 {
			t.Errorf("stored %v s: finder asked %d times, searched %d times, want no re-recording pass", storedSeconds, len(finder.queries), len(sources.requests))
		}
		if row := golden.row(); row.AudioRef == nil || *row.AudioRef != golden.ref {
			t.Errorf("stored %v s: audio_ref = %v, want the stored %q kept", storedSeconds, row.AudioRef, golden.ref)
		}
	}
}

func TestRerecording_TheSecondSearchCarriesNoPinOrKnownURLOfTheOriginal(t *testing.T) {
	pins := []ports.CatalogSource{
		{Provider: "soundcloud", ExternalID: "reckless", URL: "https://soundcloud.com/bryanadams/its-only-love"},
		{Provider: string(ports.ProviderYouTube), ExternalID: "recklessIOL"},
	}
	golden := seedItsOnlyLove(t, 0, pins)
	sources := &itsOnlyLoveSources{}
	if err := acquireFromAKnownSourceURL(context.Background(), golden, sources); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var rerecordingSearches int
	for _, req := range sources.requests {
		if req.Title != goldenRerecording().Title {
			if len(req.CatalogSources) != 2 || req.KnownSourceURL == "" {
				t.Errorf("original request = %+v, want it to carry the original's pins and known URL", req)
			}
			continue
		}
		rerecordingSearches++
		if len(req.CatalogSources) != 0 || req.KnownSourceURL != "" {
			t.Errorf("re-recording request carries pins %+v / known URL %q, want none of the original's", req.CatalogSources, req.KnownSourceURL)
		}
	}
	if rerecordingSearches == 0 {
		t.Fatalf("requests = %+v, want a search for the re-recording", sources.requests)
	}
}

func acquireFromAKnownSourceURL(ctx context.Context, golden itsOnlyLoveTrack, sources *itsOnlyLoveSources) error {
	row := golden.row()
	useSource(&row, "https://soundcloud.com/bryanadams/its-only-love")
	golden.book.Seed(row)
	return golden.service(sources, findsGoldenRerecording()).Execute(ctx, golden.userId, golden.trackId)
}

func TestRerecording_AReplaceOfAGradeFFileTakesTheRerecording(t *testing.T) {
	golden := seedItsOnlyLove(t, 195, nil)
	row := golden.row()
	row.Evidence = json.RawMessage(`{"grade":"F"}`)
	golden.book.Seed(row)

	if err := golden.service(&itsOnlyLoveSources{}, findsGoldenRerecording()).ExecuteReplace(context.Background(), golden.userId, golden.trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the grade F file replaced by the re-recording", err)
	}

	if len(golden.ledger.completed) != 1 || golden.ledger.completed[0].Relabel == nil {
		t.Fatalf("completions = %+v, want one relabelled as the re-recording", golden.ledger.completed)
	}
}

const recklessDRMURL = "https://music.youtube.com/watch?v=drmCopyIOLx"

func TestRerecording_EachPassRemembersItsRejectionsOnItsOwnBasis(t *testing.T) {
	for _, sources := range []*itsOnlyLoveSources{{}, {rerecordingBroken: true}} {
		golden := seedItsOnlyLove(t, 0, nil)
		rejections := &fakeRejectionStore{}
		svc := golden.service(sources, findsGoldenRerecording(), withServiceRejections(rejections))

		_ = svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

		var drm []ports.CandidateRejectionRecord
		for _, rec := range rejections.recorded {
			if rec.SourceKey == sourcekey.SourceKey(recklessDRMURL) {
				drm = append(drm, rec)
			}
		}
		want := ports.RejectionBasis{ExpectedSeconds: 195, ReferenceMBID: recklessMBID}
		if len(drm) != 1 || drm[0].Basis != want {
			t.Errorf("re-recording broken %v: DRM copy records = %+v, want one judged on the original's basis %+v", sources.rerecordingBroken, drm, want)
		}
	}
}

type probeOriginalOutageSource struct{ requests []ports.FindRequest }

func (*probeOriginalOutageSource) Name() acqdomain.SourceName { return "probe-outage" }

func (s *probeOriginalOutageSource) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.requests = append(s.requests, req)
	if req.Title == goldenRerecording().Title {
		return nil, nil
	}
	return nil, &acqdomain.SourceUnavailableError{Source: "probe-outage", Err: errors.New("http error 503")}
}

func (*probeOriginalOutageSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("never fetched")
}

func TestRerecording_AnOutageOnOneSourceWithNoMatchOnTheOthersTakesNoRerecording(t *testing.T) {
	golden := seedItsOnlyLove(t, 0, nil)
	finder := findsGoldenRerecording()
	outage := &probeOriginalOutageSource{}
	others := &itsOnlyLoveSources{}
	svc := newTestAcquireService(golden.ledger, service.NewSourceRegistry(outage, others), golden.store,
		withServiceResolver(pinnedRecordingResolver{}),
		withServiceIdentifier(&stubIdentifier{}),
		withServiceRerecordings(finder))

	_ = svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

	if len(finder.queries) != 0 {
		t.Errorf("finder queries = %+v, want none after a source outage", finder.queries)
	}
	for _, req := range others.requests {
		if req.Title == goldenRerecording().Title {
			t.Errorf("searched %q after an outage, want no re-recording pass", req.Title)
		}
	}
	if len(golden.ledger.completed) != 0 {
		t.Errorf("completions = %+v, want none", golden.ledger.completed)
	}
}

func TestRerecording_AReplaceWhoseRerecordingDownloadFailsLeavesTheTrackAsItWas(t *testing.T) {
	golden := seedItsOnlyLove(t, 292, nil)
	sources := &itsOnlyLoveSources{rerecordingBroken: true}
	finder := findsGoldenRerecording()
	svc := golden.service(sources, finder)

	for run := 1; run <= 2; run++ {
		err := svc.ExecuteReplace(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

		if got := failureCodeOf(err); got != downloadfailure.FailureNoConfidentMatch {
			t.Errorf("run %d: failure code = %q (%v), want no_confident_match", run, got, err)
		}
		row := golden.row()
		if row.Title != "It's Only Love" || row.Album != "Reckless" {
			t.Errorf("run %d: track = %q / %q, want the original's labels kept", run, row.Title, row.Album)
		}
		if row.ISRC == nil || *row.ISRC != "USAM18490008" || row.CatalogMBID == nil || *row.CatalogMBID != recklessMBID {
			t.Errorf("run %d: ISRC %v MBID %v, want the Reckless recording's", run, row.ISRC, row.CatalogMBID)
		}
		if row.AudioRef == nil || *row.AudioRef != golden.ref || golden.store.objects[golden.ref] != "stored bytes" {
			t.Errorf("run %d: audio_ref = %v, want the stored %q kept", run, row.AudioRef, golden.ref)
		}
		if len(finder.queries) != run || finder.queries[run-1].OriginalMBID != recklessMBID {
			t.Errorf("run %d: finder queries = %+v, want each run to ask for the Reckless recording", run, finder.queries)
		}
	}
	if len(golden.ledger.completed) != 0 {
		t.Errorf("completions = %+v, want none", golden.ledger.completed)
	}
}

type failingPinDownloads struct{}

func (failingPinDownloads) MarkUnplayable(context.Context, []ports.AudioCandidate) {}

func (failingPinDownloads) Download(context.Context, string, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("fetch: http error 403")
}

func TestRerecording_AnOriginalWhoseSoundCloudPinFailsToDownloadNeverAsksForARerecording(t *testing.T) {
	pins := []ports.CatalogSource{{Provider: "soundcloud", ExternalID: "1", URL: "https://soundcloud.com/bryanadams/its-only-love-reckless"}}
	golden := seedItsOnlyLove(t, 0, pins)
	sources := &itsOnlyLoveSources{}
	finder := findsGoldenRerecording()
	svc := newTestAcquireService(golden.ledger, service.NewSourceRegistry(savedlink.NewSource(failingPinDownloads{}), sources), golden.store,
		withServiceResolver(pinnedRecordingResolver{}),
		withServiceIdentifier(&stubIdentifier{}),
		withServiceRerecordings(finder))

	err := svc.Execute(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

	if got := failureCodeOf(err); got != downloadfailure.FailureDownloadFailed {
		t.Errorf("failure code = %q (%v), want the original's download_failed kept", got, err)
	}
	if len(finder.queries) != 0 {
		t.Errorf("finder queries = %+v, want none: a source has the original", finder.queries)
	}
	if row := golden.row(); row.AudioRef != nil {
		t.Errorf("audio ref = %v, want none stored", row.AudioRef)
	}
}

func TestRerecording_ALabelNamingTheTrackItselfRunsNoSecondPass(t *testing.T) {
	golden := seedItsOnlyLove(t, 0, nil)
	row := golden.row()
	same := goldenRerecording().MBID
	row.CatalogMBID = &same
	golden.book.Seed(row)
	sources := &itsOnlyLoveSources{}

	err := golden.service(sources, findsGoldenRerecording()).Execute(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), golden.userId, golden.trackId)

	if got := failureCodeOf(err); got != downloadfailure.FailureNoConfidentMatch {
		t.Errorf("failure code = %q (%v), want no_confident_match", got, err)
	}
	for _, req := range sources.requests {
		if req.Title == goldenRerecording().Title {
			t.Errorf("searched %q for a track already on that recording, want no second pass", req.Title)
		}
	}
	if len(golden.ledger.completed) != 0 {
		t.Errorf("completions = %+v, want none", golden.ledger.completed)
	}
}

type probeCancellingSource struct {
	*itsOnlyLoveSources
	cancel context.CancelFunc
}

func (s probeCancellingSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.requests = append(s.requests, req)
	s.cancel()
	return nil, nil
}

func TestRerecording_AJobCancelledDuringTheFirstPassNeverAsksForARerecording(t *testing.T) {
	golden := seedItsOnlyLove(t, 0, nil)
	finder := findsGoldenRerecording()
	ctx, cancel := context.WithCancel(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}))
	defer cancel()
	sources := probeCancellingSource{&itsOnlyLoveSources{}, cancel}
	svc := newTestAcquireService(golden.ledger, service.NewSourceRegistry(sources), golden.store,
		withServiceResolver(pinnedRecordingResolver{}),
		withServiceIdentifier(&stubIdentifier{}),
		withServiceRerecordings(finder))

	_ = svc.Execute(ctx, golden.userId, golden.trackId)

	if len(finder.queries) != 0 {
		t.Errorf("finder queries = %+v, want none once the job is cancelled", finder.queries)
	}
	if len(golden.ledger.completed) != 0 {
		t.Errorf("completions = %+v, want none", golden.ledger.completed)
	}
}

func TestRerecording_TheRerecordingIsTaggedAndStoredUnderItsOwnLabels(t *testing.T) {
	golden := seedItsOnlyLove(t, 0, nil)
	var tags ports.TrackTags
	svc := golden.service(&itsOnlyLoveSources{}, findsGoldenRerecording(), withServiceTagger(&capturingTagger{tags: &tags}))

	if err := svc.Execute(context.Background(), golden.userId, golden.trackId); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if tags.Title != "It’s Only Love (classic version)" || tags.Album != "It’s Only Love" || tags.Year != 2025 || tags.ISRC != "IETLH2500023" {
		t.Errorf("tags = %+v, want the classic version's title, album, year and ISRC", tags)
	}
	row := golden.row()
	if row.Album != "It’s Only Love" || row.Year == nil || *row.Year != 2025 || row.ISRC == nil || *row.ISRC != "IETLH2500023" {
		t.Errorf("track album %q year %v ISRC %v, want the classic version's", row.Album, row.Year, row.ISRC)
	}
}

type probeBlockingRerecordingSource struct{ *itsOnlyLoveSources }

func (s probeBlockingRerecordingSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	if req.Title == goldenRerecording().Title {
		s.requests = append(s.requests, req)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.itsOnlyLoveSources.Find(ctx, req)
}

func TestRerecording_ASecondPassThatRunsOutOfTimeFailsAsATimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		golden := seedItsOnlyLove(t, 292, nil)
		sources := probeBlockingRerecordingSource{&itsOnlyLoveSources{}}
		svc := newTestAcquireService(golden.ledger, service.NewSourceRegistry(sources), golden.store,
			withServiceResolver(pinnedRecordingResolver{}),
			withServiceIdentifier(&stubIdentifier{}),
			withServiceRerecordings(findsGoldenRerecording()))
		ctx, cancel := context.WithTimeout(service.WithJobRun(context.Background(), service.JobRun{Attempt: service.MaxAcquisitionAttempts}), 3*time.Second)
		defer cancel()

		err := svc.ExecuteReplace(ctx, golden.userId, golden.trackId)

		reached := false
		for _, req := range sources.requests {
			reached = reached || req.Title == goldenRerecording().Title
		}
		if !reached {
			t.Fatalf("requests = %+v, want the deadline to land in the re-recording pass", sources.requests)
		}
		if got := failureCodeOf(err); got != downloadfailure.FailureAcquisitionCancelled && got != downloadfailure.FailureSourceUnavailable {
			t.Errorf("failure code = %q (%v), want a timeout, not the original's no_confident_match", got, err)
		}
		if row := golden.row(); row.AudioRef == nil || *row.AudioRef != golden.ref || row.Title != "It's Only Love" {
			t.Errorf("track = %q on %v, want the stored file and labels kept", row.Title, row.AudioRef)
		}
	})
}

type capturingTagger struct{ tags *ports.TrackTags }

func (r *capturingTagger) Tag(_ context.Context, _ string, tags ports.TrackTags) error {
	*r.tags = tags
	return nil
}

type findStubSource struct {
	name     acqdomain.SourceName
	found    []ports.AudioCandidate
	fetchErr error
	finds    int
}

func (s *findStubSource) Name() acqdomain.SourceName { return s.name }

func (s *findStubSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.finds++
	return s.found, nil
}

func (s *findStubSource) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	return fetchedFrom("", s.fetchErr)
}

func titledCandidate(url string) ports.AudioCandidate {
	return ports.AudioCandidate{Title: "Blinding Lights", URL: url}
}

func fetchedFrom(path string, err error) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{Path: path}, err
}

type portsOption func(*service.StagePorts, *service.VerificationPorts)

func newTestAcquireService(
	ledger ports.LibraryTracks,
	sources *service.SourceRegistry,
	audioStore ports.AudioWriter,
	opts ...any,
) *service.AcquireTrackAudioService {
	return newTestAcquireServiceIn(osWorkspace{}, ledger, sources, audioStore, opts...)
}

func newTestAcquireServiceIn(
	workspace ports.ScratchFolder,
	ledger ports.LibraryTracks,
	sources *service.SourceRegistry,
	audioStore ports.AudioWriter,
	opts ...any,
) *service.AcquireTrackAudioService {
	stages := noopStages()
	stages.Orphans = service.MustDirectDeleteOrphans(audioStore)
	wiring := noopVerification()
	var public []func(*service.AcquireTrackAudioService)
	for _, opt := range opts {
		switch opt := opt.(type) {
		case portsOption:
			opt(&stages, &wiring)
		case func(*service.AcquireTrackAudioService):
			public = append(public, opt)
		default:
			panic(fmt.Sprintf("newTestAcquireService: unsupported option %T", opt))
		}
	}
	return service.MustNewAcquireTrackAudioService(ledger, sources, audioStore, workspace, wiring, stages, public...)
}

type discardingDeleter struct{}

func (discardingDeleter) Delete(context.Context, string) error { return nil }

func noopStages() service.StagePorts {
	return service.StagePorts{
		Prober:        ports.NoopAudioProber(),
		Tagger:        ports.NoopAudioTagger(),
		Rejections:    ports.NoopRejectionStore(),
		StoredAudio:   ports.NoopStoredAudioReader(),
		Orphans:       service.MustDirectDeleteOrphans(discardingDeleter{}),
		Skips:         ports.NoopVerifySkipRecorder(),
		Events:        events.NoopPublisher(),
		DownloadLimit: service.Unlimited(),
	}
}

func withServiceEvents(pub events.Publisher) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { stages.Events = pub }
}

func withServiceResolver(r ports.RecordingResolver) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { wiring.Recordings = r }
}

func withServiceProber(p ports.AudioProber) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { stages.Prober = p }
}

func withServiceTagger(t ports.AudioTagger) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { stages.Tagger = t }
}

func withServiceRejections(r ports.RejectionStore) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { stages.Rejections = r }
}

func withServiceStoredAudio(r ports.StoredAudioReader) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { stages.StoredAudio = r }
}

func noopVerification() service.VerificationPorts {
	return service.VerificationPorts{
		Identifier:      ports.NoopAudioIdentifier(),
		RecordingLookup: ports.NoopRecordingLookup(),
		Rerecordings:    ports.NoopRerecordingFinder(),
		Previews:        ports.NoopPreviewMatcher(),
		Recordings:      ports.NoopRecordingResolver(),
	}
}

func withServiceIdentifier(i ports.AudioIdentifier) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { wiring.Identifier = i }
}

func withServiceRerecordings(f ports.RerecordingFinder) portsOption {
	return func(stages *service.StagePorts, wiring *service.VerificationPorts) { wiring.Rerecordings = f }
}

type stepPorts struct {
	identifier ports.AudioIdentifier
	lookup     ports.RecordingLookup
	prober     ports.AudioProber
	skips      ports.VerifySkipRecorder
	limit      *service.ConcurrencyLimit
}

type stepPortsOption func(*stepPorts)

type fetcherPort interface {
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error)
}

func newTestDownloadStep(fetcher fetcherPort, opts ...any) *service.DownloadStep {
	wired := stepPorts{
		identifier: ports.NoopAudioIdentifier(),
		lookup:     ports.NoopRecordingLookup(),
		prober:     ports.NoopAudioProber(),
		skips:      ports.NoopVerifySkipRecorder(),
		limit:      service.Unlimited(),
	}
	var public []func(*service.DownloadStep)
	for _, opt := range opts {
		switch opt := opt.(type) {
		case stepPortsOption:
			opt(&wired)
		case func(*service.DownloadStep):
			public = append(public, opt)
		default:
			panic(fmt.Sprintf("newTestDownloadStep: unsupported option %T", opt))
		}
	}
	return service.MustNewDownloadStep(fetcher, osWorkspace{}, wired.identifier, wired.lookup, wired.prober, wired.skips, wired.limit, public...)
}

func withTestLimit(l *service.ConcurrencyLimit) stepPortsOption {
	return func(w *stepPorts) { w.limit = l }
}

func withTestProber(p ports.AudioProber) stepPortsOption {
	return func(w *stepPorts) { w.prober = p }
}

func withTestSkips(r ports.VerifySkipRecorder) stepPortsOption {
	return func(w *stepPorts) { w.skips = r }
}

func withTestIdentifier(i ports.AudioIdentifier) stepPortsOption {
	return func(w *stepPorts) { w.identifier = i }
}

func withTestLookup(l ports.RecordingLookup) stepPortsOption {
	return func(w *stepPorts) { w.lookup = l }
}

type creationRecordingWorkspace struct {
	inner ports.ScratchFolder

	mu       sync.Mutex
	patterns []string
	created  []string
	removed  []string
}

func newCreationRecordingWorkspace() *creationRecordingWorkspace {
	return &creationRecordingWorkspace{inner: osWorkspace{}}
}

func (r *creationRecordingWorkspace) Create(pattern string) (string, error) {
	dir, err := r.inner.Create(pattern)
	if err == nil {
		r.mu.Lock()
		r.patterns = append(r.patterns, pattern)
		r.created = append(r.created, dir)
		r.mu.Unlock()
	}
	return dir, err
}

func (r *creationRecordingWorkspace) Remove(path string) error {
	r.mu.Lock()
	r.removed = append(r.removed, path)
	r.mu.Unlock()
	return r.inner.Remove(path)
}

func (r *creationRecordingWorkspace) SweepAbandoned(t time.Time) int {
	return r.inner.SweepAbandoned(t)
}

func (r *creationRecordingWorkspace) Size(path string) (int64, error) { return r.inner.Size(path) }

func (r *creationRecordingWorkspace) createdDirs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.created...)
}

func (r *creationRecordingWorkspace) removedPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.removed...)
}

func (r *creationRecordingWorkspace) removedCount(path string) int {
	n := 0
	for _, p := range r.removedPaths() {
		if p == path {
			n++
		}
	}
	return n
}

func (r *creationRecordingWorkspace) liveDirs() []string {
	var live []string
	for _, dir := range r.createdDirs() {
		if r.removedCount(dir) == 0 {
			live = append(live, dir)
		}
	}
	return live
}

func outageSource() findOnlySource {
	return findOnlySource{name: "down", err: &acqdomain.SourceUnavailableError{Source: "down", Err: errors.New("429")}}
}

func TestAcquireOptions_RefuseANilContextObserver(t *testing.T) {
	_, err := service.NewAcquireTrackAudioService(acquisitiontest.NewLedger(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), osWorkspace{}, noopVerification(), noopStages(), service.WithContextObserver(nil))

	if !errors.Is(err, service.ErrMissingCollaborator) {
		t.Errorf("error = %v, want ErrMissingCollaborator", err)
	}
}
