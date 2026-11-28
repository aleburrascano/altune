package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

var errDeleteFailed = errors.New("object store unavailable")

type recordingStore struct {
	*objectStore
	mu          sync.Mutex
	writes      []string
	deleteCalls int
	failDeletes int
}

func newRecordingStore(held ...string) *recordingStore {
	objects := make(map[string]string, len(held))
	for _, ref := range held {
		objects[ref] = "bytes"
	}
	return &recordingStore{objectStore: &objectStore{objects: objects}}
}

func (s *recordingStore) Store(ctx context.Context, sourcePath, audioRef string) error {
	s.mu.Lock()
	s.writes = append(s.writes, audioRef)
	s.mu.Unlock()
	return s.objectStore.Store(ctx, sourcePath, audioRef)
}

func (s *recordingStore) Delete(ctx context.Context, audioRef string) error {
	s.mu.Lock()
	s.deleteCalls++
	failing := s.deleteCalls <= s.failDeletes
	s.mu.Unlock()
	if failing {
		return errDeleteFailed
	}
	return s.objectStore.Delete(ctx, audioRef)
}

func (s *recordingStore) wroteTo(audioRef string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.writes, audioRef)
}

type orphanLog struct {
	mu       sync.Mutex
	recorded []ports.OrphanedAudio
}

func (q *orphanLog) RecordOrphanedAudio(_ context.Context, o ports.OrphanedAudio) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.recorded = append(q.recorded, o)
	return nil
}

type refUsageLedger struct {
	*memory.Ledger
	mu    sync.Mutex
	calls int
	usage func(call int) (bool, error)
}

func (l *refUsageLedger) Requeue(context.Context, ids.TrackId) error {
	return nil
}

func (l *refUsageLedger) AudioRefInUse(ctx context.Context, audioRef string, exclude ids.TrackId) (bool, error) {
	l.mu.Lock()
	l.calls++
	call := l.calls
	l.mu.Unlock()
	if l.usage == nil {
		return l.Ledger.AudioRefInUse(ctx, audioRef, exclude)
	}
	return l.usage(call)
}

type cancellingCompleteLedger struct {
	*memory.Ledger
	cancel context.CancelFunc
}

func (l cancellingCompleteLedger) Complete(context.Context, ids.TrackId, ids.UserId, ports.ClaimNumber, ports.Completion) error {
	l.cancel()
	return errors.New("scheduler shutting down")
}

type cancellingStore struct {
	*objectStore
	cancel context.CancelFunc
}

func (s cancellingStore) Store(context.Context, string, string) error {
	s.cancel()
	return errors.New("put object: connection reset")
}

type decodeFailingFromTheSecondCheck struct {
	mu     sync.Mutex
	checks int
}

func (p *decodeFailingFromTheSecondCheck) ProbeDuration(context.Context, string) (float64, error) {
	return 0, nil
}

func (p *decodeFailingFromTheSecondCheck) ValidateDecodable(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checks++
	if p.checks > 1 {
		return errors.New("audio stream failed to decode")
	}
	return nil
}

type topicSource struct {
	freshAudioSource
	title, artist string
}

func (s topicSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{downloadtest.Candidate(
		"https://www.youtube.com/watch?v=freshAAAAAA",
		downloadtest.WithTitle(s.title),
		downloadtest.WithChannel(s.artist+" - Topic"),
		downloadtest.WithCategories([]string{"Music"}),
	)}, nil
}

func sourceFor(track memory.LedgerTrack) *service.SourceRegistry {
	return service.NewSourceRegistry(topicSource{title: track.Title, artist: track.Artist})
}

type storing struct {
	t      *testing.T
	ledger ports.LibraryTracks
	store  ports.AudioWriter
	track  memory.LedgerTrack
}

func (s storing) execute(opts ...func(*service.AcquireTrackAudioService)) error {
	s.t.Helper()
	return s.executeStaged(noStages(deletingFrom(s.store)), opts...)
}

func (s storing) executeStaged(stages service.StagePorts, opts ...func(*service.AcquireTrackAudioService)) error {
	s.t.Helper()
	svc := mustAcquisitionServices(s.ledger, sourceFor(s.track), s.store, scratchfiles.New(), noVerification(), stages, opts...)
	return svc.Execute(context.Background(), ports.AcquisitionJob{UserID: s.track.UserId, TrackID: s.track.ID})
}

func pending(title, artist, album string) memory.LedgerTrack {
	return memory.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: title, Artist: artist, Album: album, Status: ports.TrackPending,
	}}
}

func refOf(track memory.LedgerTrack, prefix string) string {
	return prefix + service.BuildAudioRef(service.TrackRef{
		ID: track.ID.String(), UserID: track.UserId.String(),
		Title: track.Title, Artist: track.Artist, Album: track.Album,
	}, "audio.mp3")
}

func servedBy(track memory.LedgerTrack, audioRef string) memory.LedgerTrack {
	serving := pending(track.Title, track.Artist, track.Album)
	serving.UserId = track.UserId
	serving.Status, serving.AudioRef, serving.Streamable = ports.TrackReady, &audioRef, true
	return serving
}

func storedRef(t *testing.T, ledger *memory.Ledger, id ids.TrackId) string {
	t.Helper()
	got, ok := ledger.Track(id)
	if !ok || got.AudioRef == nil {
		t.Fatalf("track %s holds no audio ref: %+v", id, got)
	}
	return *got.AudioRef
}

func TestStore_ANewTrackIsStoredAtItsCanonicalRef(t *testing.T) {
	track := pending("Song Title", "Artist Name", "Album Name")
	ledger, store := memory.NewLedger(track), newRecordingStore()

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := track.UserId.String() + "/artist name/album name/song title.mp3"
	if got := storedRef(t, ledger, track.ID); got != want {
		t.Errorf("audio ref = %q, want %q", got, want)
	}
	if !store.holds(want) {
		t.Errorf("the object store does not hold %q", want)
	}
}

func TestStore_TheKeyPrefixLeadsTheStoredRef(t *testing.T) {
	track := pending("Song Title", "Artist Name", "Album Name")
	ledger, store := memory.NewLedger(track), newRecordingStore()

	if err := (storing{t, ledger, store, track}).execute(service.WithAcquireStoreKeyPrefix("staging/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "staging/" + track.UserId.String() + "/artist name/album name/song title.mp3"
	if got := storedRef(t, ledger, track.ID); got != want || !store.holds(want) {
		t.Errorf("audio ref = %q, stored = %v, want %q stored", got, store.holds(want), want)
	}
}

var stagedReplaceRef = regexp.MustCompile(`^staging/.*/blinding lights\.replace-[0-9a-f-]{36}\.mp3$`)

func TestStore_AReplaceStagesItsOwnPrefixedRef(t *testing.T) {
	served := seedReadyTrack(t)
	ledger, store := served.ledger, newRecordingStore(served.originalRef)

	svc := mustAcquisitionServices(ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		scratchfiles.New(), noVerification(), noStages(), service.WithAcquireStoreKeyPrefix("staging/"))
	if err := svc.ExecuteReplace(context.Background(), ports.AcquisitionJob{UserID: served.userID, TrackID: served.trackID}); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if got := storedRef(t, ledger, served.trackID); !stagedReplaceRef.MatchString(got) {
		t.Errorf("audio ref = %q, want a staged replace ref under the prefix", got)
	}
}

func TestStore_AFileTheStoreCannotDecodeIsNeverStored(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := memory.NewLedger(track), newRecordingStore()

	err := (storing{t, ledger, store, track}).executeStaged(noStages(probedBy(&decodeFailingFromTheSecondCheck{}), deletingFrom(store)))

	var stepErr *service.StepError
	if !errors.As(err, &stepErr) || stepErr.Step != "store" {
		t.Fatalf("Execute = %v, want a StepError from the store step", err)
	}
	if len(store.writes) != 0 {
		t.Errorf("writes = %v, want nothing stored when validation fails", store.writes)
	}
}

func TestStore_AFailedStoreWriteIsReportedAsTheStoreStep(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	store := newRecordingStore()
	store.storeErr = errors.New("storage unavailable")

	err := (storing{t, memory.NewLedger(track), store, track}).execute()

	var stepErr *service.StepError
	if !errors.As(err, &stepErr) || stepErr.Step != "store" {
		t.Fatalf("Execute = %v, want a StepError from the store step", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("objects = %v, want none", store.objects)
	}
}

func TestStore_ACancelledWriteSettlesTheTrackAsCancelled(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger := memory.NewLedger(track)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := cancellingStore{objectStore: &objectStore{objects: map[string]string{}}, cancel: cancel}
	svc := mustAcquisitionServices(ledger, sourceFor(track), store,
		scratchfiles.New(), noVerification(), noStages())

	if err := svc.Execute(ctx, ports.AcquisitionJob{UserID: track.UserId, TrackID: track.ID}); err == nil {
		t.Fatal("Execute succeeded, want the cancelled store to fail")
	}

	got, _ := ledger.Track(track.ID)
	if got.Status != ports.TrackFailed || got.FailureReason != string(downloadfailure.FailureAcquisitionCancelled) {
		t.Errorf("track = %v %q, want failed as %q", got.Status, got.FailureReason, downloadfailure.FailureAcquisitionCancelled)
	}
}

func TestBuildAudioRefNormalizesCaseAndUnicode(t *testing.T) {
	cases := []struct {
		name string
		a    service.TrackRef
		b    service.TrackRef
	}{
		{
			name: "case fold",
			a:    service.TrackRef{UserID: "u1", Artist: "Daft Punk", Album: "Discovery", Title: "One More Time"},
			b:    service.TrackRef{UserID: "u1", Artist: "DAFT PUNK", Album: "DISCOVERY", Title: "ONE MORE TIME"},
		},
		{
			name: "unicode composition (precomposed vs decomposed é)",
			a:    service.TrackRef{UserID: "u1", Artist: "Beyoncé", Album: "Lemonade", Title: "Sorry"},
			b:    service.TrackRef{UserID: "u1", Artist: "Beyoncé", Album: "Lemonade", Title: "Sorry"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := service.BuildAudioRef(tc.a, "track.mp3")
			want := service.BuildAudioRef(tc.b, "track.mp3")
			if got != want {
				t.Fatalf("storage paths diverge for equivalent metadata:\n a = %q\n b = %q", got, want)
			}
		})
	}
}

func TestStore_NeverOverwritesAudioAnotherTrackServes(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	serving := servedBy(track, canonical)
	ledger, store := memory.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := domain.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want {
		t.Errorf("audio ref = %q, want %q", got, want)
	}
	if store.wroteTo(canonical) || len(store.writes) != 1 {
		t.Errorf("writes = %v, want only %q: track %s serves %q", store.writes, want, serving.ID, canonical)
	}
}

func TestStore_KeepsTheCanonicalRefTheTrackAlreadyHolds(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	track.AudioRef = &canonical
	ledger, store := memory.NewLedger(track), newRecordingStore()

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := storedRef(t, ledger, track.ID); got != canonical {
		t.Errorf("audio ref = %q, want %q", got, canonical)
	}
}

func TestStore_KeepsTheCanonicalRefWhenNoTrackHoldsIt(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	ledger, store := memory.NewLedger(track), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := storedRef(t, ledger, track.ID); got != canonical {
		t.Errorf("audio ref = %q, want the canonical %q", got, canonical)
	}
}

func TestStore_ScopesTheRefWhenUsageIsUnknown(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	ledger := &refUsageLedger{Ledger: memory.NewLedger(track), usage: func(int) (bool, error) {
		return false, errors.New("database unavailable")
	}}

	if err := (storing{t, ledger, newRecordingStore(), track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := domain.TrackScoped(refOf(track, ""), track.ID)
	if got := storedRef(t, ledger.Ledger, track.ID); got != want {
		t.Errorf("audio ref = %q, want %q", got, want)
	}
}

func TestStore_ScopesTheRefWhenAnotherTrackSharesItWithTheAcquiringTrack(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	track.AudioRef = &canonical
	serving := servedBy(track, canonical)
	ledger, store := memory.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := domain.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want {
		t.Errorf("audio ref = %q, want %q", got, want)
	}
	if store.wroteTo(canonical) {
		t.Errorf("wrote %q, which track %s still serves", canonical, serving.ID)
	}
}

func TestStore_ScopesTheRefWhenAnAmpersandVariantServesTheCanonicalRef(t *testing.T) {
	track := pending("Cecilia", "Simon & Garfunkel", "X")
	canonical := refOf(track, "")
	serving := servedBy(track, canonical)
	ledger, store := memory.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(canonical, "simon and garfunkel") {
		t.Fatalf("canonical ref %q does not spell the ampersand out", canonical)
	}
	want := domain.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want only %q", got, store.writes, want)
	}
}

func TestStore_ScopesThePrefixedRefAnotherTrackServes(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "staging/")
	serving := servedBy(track, canonical)
	ledger, store := memory.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(service.WithAcquireStoreKeyPrefix("staging/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := domain.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want only %q", got, store.writes, want)
	}
}

func TestStore_ARetryUnderACollisionReusesOneScopedRef(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	ledger, store := memory.NewLedger(track, servedBy(track, canonical)), newRecordingStore(canonical)
	acquire := storing{t, ledger, store, track}

	ledger.FailWrites(errors.New("database unavailable"))
	if err := acquire.execute(); err == nil {
		t.Fatal("first Execute succeeded, want the failed commit")
	}
	ledger.FailWrites(nil)
	if err := acquire.execute(); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	scoped := domain.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != scoped {
		t.Errorf("audio ref = %q, want %q", got, scoped)
	}
	if store.wroteTo(canonical) {
		t.Errorf("writes = %v, want %q never written", store.writes, canonical)
	}
	if got := len(store.writes); got != 2 {
		t.Errorf("writes = %v, want the scoped ref written once per attempt", store.writes)
	}
}

func TestStore_AReplaceStagesItsOwnRefEvenWhenAnotherTrackServesTheCanonicalRef(t *testing.T) {
	served := seedReadyTrack(t)
	twin := servedBy(memory.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: served.userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
	}}, served.originalRef)
	served.ledger.Seed(twin)
	store := newRecordingStore(served.originalRef)
	svc := mustAcquisitionServices(served.ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		scratchfiles.New(), noVerification(), noStages())

	if err := svc.ExecuteReplace(context.Background(), ports.AcquisitionJob{UserID: served.userID, TrackID: served.trackID}); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	got := storedRef(t, served.ledger, served.trackID)
	if !strings.Contains(got, ".replace-") || store.wroteTo(served.originalRef) || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want one write to a staged ref, never %q", got, store.writes, served.originalRef)
	}
}

func TestStoreRollback_DeletesTheAudioWhenTheCommitFails(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := memory.NewLedger(track), newRecordingStore()
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if len(store.writes) != 1 {
		t.Fatalf("writes = %v, want the audio stored once before the commit failed", store.writes)
	}
	if len(store.objects) != 0 {
		t.Errorf("objects = %v, want the rolled-back audio deleted", store.objects)
	}
}

func TestStoreRollback_DeletesAFreshlyStagedReplaceAndKeepsTheServedAudio(t *testing.T) {
	served := seedReadyTrack(t)
	store := newRecordingStore(served.originalRef)
	served.ledger.FailWrites(errors.New("database unavailable"))
	svc := mustAcquisitionServices(served.ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		scratchfiles.New(), noVerification(), noStages())

	if err := svc.ExecuteReplace(context.Background(), ports.AcquisitionJob{UserID: served.userID, TrackID: served.trackID}); err == nil {
		t.Fatal("ExecuteReplace succeeded, want the failed commit")
	}

	if len(store.writes) != 1 {
		t.Fatalf("writes = %v, want one staged write before the commit failed", store.writes)
	}
	if !store.holds(served.originalRef) || len(store.objects) != 1 {
		t.Errorf("objects = %v, want only the served %q", store.objects, served.originalRef)
	}
}

func TestStoreRollback_OfAScopedStoreDeletesItAndKeepsTheServedAudio(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	ledger, store := memory.NewLedger(track, servedBy(track, canonical)), newRecordingStore(canonical)
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if len(store.writes) != 1 {
		t.Fatalf("writes = %v, want one scoped write before the commit failed", store.writes)
	}
	if !store.holds(canonical) || len(store.objects) != 1 {
		t.Errorf("objects = %v, want only the served %q: the scoped write is rolled back", store.objects, canonical)
	}
}

func TestStoreRollback_RetriesATransientDeleteFailure(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := memory.NewLedger(track), newRecordingStore()
	store.failDeletes = 2
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if len(store.objects) != 0 || store.deleteCalls != 3 {
		t.Errorf("objects = %v after %d delete calls, want the audio deleted on the third", store.objects, store.deleteCalls)
	}
}

func TestStoreRollback_WaitsLongerBeforeEachRetryOfTheDelete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &timingStore{recordingStore: newRecordingStore()}
		store.failDeletes = 1 << 30
		step := mustNewStoreStep(store, scratchfiles.New(), downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(store), downloadtest.SilentAudioRefLookup())

		err := step.Rollback(context.Background(), &service.AcquisitionContext{AudioRef: "u/song.mp3"})

		if err == nil {
			t.Fatal("Rollback succeeded, want the permanent delete failure")
		}
		ceilings := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}
		if len(store.at) < len(ceilings)+1 {
			t.Fatalf("delete attempts = %d, want at least %d (the first try and one per retry)", len(store.at), len(ceilings)+1)
		}
		for i, ceiling := range ceilings {
			wait := store.at[i+1].Sub(store.at[i])
			if wait < ceiling/2 || wait > ceiling {
				t.Errorf("wait %d = %v, want within [%v, %v]", i+1, wait, ceiling/2, ceiling)
			}
		}
	})
}

func TestStoreRollback_StopsWaitingBetweenDeleteRetriesWhenTheContextIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &timingStore{recordingStore: newRecordingStore()}
		store.failDeletes = 1 << 30
		step := mustNewStoreStep(store, scratchfiles.New(), downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(store), downloadtest.SilentAudioRefLookup())
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Millisecond, cancel)
		start := time.Now()

		err := step.Rollback(ctx, &service.AcquisitionContext{AudioRef: "u/song.mp3"})

		if err == nil {
			t.Fatal("Rollback succeeded, want the delete failure")
		}
		if waited := time.Since(start); waited > 50*time.Millisecond {
			t.Errorf("Rollback took %v after a cancel at 10ms, want it to stop waiting at once", waited)
		}
		for i, at := range store.at {
			if at.Sub(start) > 10*time.Millisecond {
				t.Errorf("delete attempt %d ran %v after the start, want none after the cancel at 10ms", i+1, at.Sub(start))
			}
		}
	})
}

type timingStore struct {
	*recordingStore
	at []time.Time
}

func (s *timingStore) Delete(ctx context.Context, audioRef string) error {
	s.at = append(s.at, time.Now())
	return s.recordingStore.Delete(ctx, audioRef)
}

func TestStoreRollback_SurfacesAPermanentDeleteFailureAndQueuesTheOrphan(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store, orphans := memory.NewLedger(track), newRecordingStore(), &orphanLog{}
	store.failDeletes = 1 << 30
	ledger.FailWrites(errors.New("database unavailable"))

	err := (storing{t, ledger, store, track}).executeStaged(noStages(orphaningInto(orphans)))

	if err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}
	if store.deleteCalls < 2 {
		t.Errorf("delete calls = %d, want the compensation retried", store.deleteCalls)
	}
	want := ports.OrphanedAudio{AudioRef: refOf(track, ""), UserId: track.UserId, TrackId: track.ID}
	if len(orphans.recorded) != 1 || orphans.recorded[0] != want {
		t.Errorf("recorded = %+v, want exactly %+v", orphans.recorded, want)
	}
}

func TestStoreRollback_KeepsAudioAnotherTrackStartedServingMeanwhile(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger := &refUsageLedger{Ledger: memory.NewLedger(track), usage: func(call int) (bool, error) { return call > 1, nil }}
	store := newRecordingStore()
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if !store.holds(refOf(track, "")) || store.deleteCalls != 0 {
		t.Errorf("objects = %v, deletes = %d, want the audio another track now serves kept", store.objects, store.deleteCalls)
	}
}

func TestStoreRollback_KeepsAudioWhenTheReferenceCheckFails(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger := &refUsageLedger{Ledger: memory.NewLedger(track), usage: func(call int) (bool, error) {
		if call > 1 {
			return false, errors.New("database unavailable")
		}
		return false, nil
	}}
	store := newRecordingStore()
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if !store.holds(refOf(track, "")) {
		t.Error("rollback deleted audio it could not tell whether a track still serves")
	}
}

func TestStoreRollback_DeletesTheAudioAfterTheJobContextIsCancelled(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger := cancellingCompleteLedger{Ledger: memory.NewLedger(track), cancel: cancel}
	store := newRecordingStore()
	svc := mustAcquisitionServices(ledger, sourceFor(track), store,
		scratchfiles.New(), noVerification(), noStages())

	if err := svc.Execute(ctx, ports.AcquisitionJob{UserID: track.UserId, TrackID: track.ID}); err == nil {
		t.Fatal("Execute succeeded, want the cancelled commit to fail")
	}

	if len(store.writes) != 1 {
		t.Fatalf("writes = %v, want the audio stored once before the commit was cancelled", store.writes)
	}
	if len(store.objects) != 0 {
		t.Errorf("objects = %v, want the audio deleted although the job context was cancelled", store.objects)
	}
}
