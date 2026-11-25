package service_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	"altune/go-api/internal/acquisition/internal/core/audioref"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

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
	*acquisitiontest.Ledger
	mu    sync.Mutex
	calls int
	usage func(call int) (bool, error)
}

func (l *refUsageLedger) AudioRefInUse(ctx context.Context, audioRef string, exclude shared.TrackId) (bool, error) {
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
	*acquisitiontest.Ledger
	cancel context.CancelFunc
}

func (l cancellingCompleteLedger) Complete(context.Context, shared.TrackId, shared.UserId, ports.Fence, ports.Completion) error {
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
	return []ports.AudioCandidate{{
		Title:      s.title,
		URL:        "https://www.youtube.com/watch?v=freshAAAAAA",
		Channel:    s.artist + " - Topic",
		Categories: []string{"Music"},
	}}, nil
}

func sourceFor(track acquisitiontest.LedgerTrack) *service.SourceRegistry {
	return service.NewSourceRegistry(topicSource{title: track.Title, artist: track.Artist})
}

type storing struct {
	t      *testing.T
	ledger ports.TrackLedger
	store  ports.AudioWriter
	track  acquisitiontest.LedgerTrack
}

func (s storing) execute(opts ...func(*service.AcquireTrackAudioService)) error {
	s.t.Helper()
	opts = append([]func(*service.AcquireTrackAudioService){service.WithAcquireTempWorkspace(tempfs.New())}, opts...)
	svc := service.NewAcquireTrackAudioService(s.ledger, sourceFor(s.track), s.store, opts...)
	return svc.Execute(context.Background(), s.track.UserId, s.track.ID)
}

func pending(title, artist, album string) acquisitiontest.LedgerTrack {
	return acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: shared.NewTrackId(), UserId: shared.NewUserId(uuid.New()),
		Title: title, Artist: artist, Album: album, Status: ports.TrackPending,
	}}
}

func refOf(track acquisitiontest.LedgerTrack, prefix string) string {
	return prefix + service.BuildAudioRef(service.TrackRef{
		ID: track.ID.String(), UserID: track.UserId.String(),
		Title: track.Title, Artist: track.Artist, Album: track.Album,
	}, "audio.mp3")
}

func servedBy(track acquisitiontest.LedgerTrack, audioRef string) acquisitiontest.LedgerTrack {
	serving := pending(track.Title, track.Artist, track.Album)
	serving.UserId = track.UserId
	serving.Status, serving.AudioRef = ports.TrackReady, &audioRef
	return serving
}

func storedRef(t *testing.T, ledger *acquisitiontest.Ledger, id shared.TrackId) string {
	t.Helper()
	got, ok := ledger.Track(id)
	if !ok || got.AudioRef == nil {
		t.Fatalf("track %s holds no audio ref: %+v", id, got)
	}
	return *got.AudioRef
}

func TestStore_ANewTrackIsStoredAtItsCanonicalRef(t *testing.T) {
	track := pending("Song Title", "Artist Name", "Album Name")
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()

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
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()

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

	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		service.WithAcquireTempWorkspace(tempfs.New()), service.WithAcquireStoreKeyPrefix("staging/"))
	if err := svc.ExecuteReplace(context.Background(), served.userID, served.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if got := storedRef(t, ledger, served.trackID); !stagedReplaceRef.MatchString(got) {
		t.Errorf("audio ref = %q, want a staged replace ref under the prefix", got)
	}
}

func TestStore_AFileTheStoreCannotDecodeIsNeverStored(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()

	err := (storing{t, ledger, store, track}).execute(service.WithAudioProber(&decodeFailingFromTheSecondCheck{}))

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

	err := (storing{t, acquisitiontest.NewLedger(track), store, track}).execute()

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
	ledger := acquisitiontest.NewLedger(track)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := cancellingStore{objectStore: &objectStore{objects: map[string]string{}}, cancel: cancel}
	svc := service.NewAcquireTrackAudioService(ledger, sourceFor(track), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	if err := svc.Execute(ctx, track.UserId, track.ID); err == nil {
		t.Fatal("Execute succeeded, want the cancelled store to fail")
	}

	got, _ := ledger.Track(track.ID)
	if got.Status != ports.TrackFailed || got.FailureReason != string(shared.FailureAcquisitionCancelled) {
		t.Errorf("track = %v %q, want failed as %q", got.Status, got.FailureReason, shared.FailureAcquisitionCancelled)
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
	ledger, store := acquisitiontest.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := audioref.TrackScoped(canonical, track.ID)
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
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()

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
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := storedRef(t, ledger, track.ID); got != canonical {
		t.Errorf("audio ref = %q, want the canonical %q", got, canonical)
	}
}

func TestStore_ScopesTheRefWhenUsageIsUnknown(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	ledger := &refUsageLedger{Ledger: acquisitiontest.NewLedger(track), usage: func(int) (bool, error) {
		return false, errors.New("database unavailable")
	}}

	if err := (storing{t, ledger, newRecordingStore(), track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := audioref.TrackScoped(refOf(track, ""), track.ID)
	if got := storedRef(t, ledger.Ledger, track.ID); got != want {
		t.Errorf("audio ref = %q, want %q", got, want)
	}
}

func TestStore_ScopesTheRefWhenAnotherTrackSharesItWithTheAcquiringTrack(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	track.AudioRef = &canonical
	serving := servedBy(track, canonical)
	ledger, store := acquisitiontest.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := audioref.TrackScoped(canonical, track.ID)
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
	ledger, store := acquisitiontest.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(canonical, "simon and garfunkel") {
		t.Fatalf("canonical ref %q does not spell the ampersand out", canonical)
	}
	want := audioref.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want only %q", got, store.writes, want)
	}
}

func TestStore_ScopesThePrefixedRefAnotherTrackServes(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "staging/")
	serving := servedBy(track, canonical)
	ledger, store := acquisitiontest.NewLedger(track, serving), newRecordingStore(canonical)

	if err := (storing{t, ledger, store, track}).execute(service.WithAcquireStoreKeyPrefix("staging/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := audioref.TrackScoped(canonical, track.ID)
	if got := storedRef(t, ledger, track.ID); got != want || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want only %q", got, store.writes, want)
	}
}

func TestStore_ARetryUnderACollisionReusesOneScopedRef(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	ledger, store := acquisitiontest.NewLedger(track, servedBy(track, canonical)), newRecordingStore(canonical)
	acquire := storing{t, ledger, store, track}

	ledger.FailWrites(errors.New("database unavailable"))
	if err := acquire.execute(); err == nil {
		t.Fatal("first Execute succeeded, want the failed commit")
	}
	ledger.FailWrites(nil)
	if err := acquire.execute(); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	scoped := audioref.TrackScoped(canonical, track.ID)
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
	twin := servedBy(acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: shared.NewTrackId(), UserId: served.userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
	}}, served.originalRef)
	served.ledger.Seed(twin)
	store := newRecordingStore(served.originalRef)
	svc := service.NewAcquireTrackAudioService(served.ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	if err := svc.ExecuteReplace(context.Background(), served.userID, served.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	got := storedRef(t, served.ledger, served.trackID)
	if !strings.Contains(got, ".replace-") || store.wroteTo(served.originalRef) || len(store.writes) != 1 {
		t.Errorf("audio ref = %q, writes = %v, want one write to a staged ref, never %q", got, store.writes, served.originalRef)
	}
}

func TestStoreRollback_DeletesTheAudioWhenTheCommitFails(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if len(store.objects) != 0 {
		t.Errorf("objects = %v, want the rolled-back audio deleted", store.objects)
	}
}

func TestStoreRollback_DeletesAFreshlyStagedReplaceAndKeepsTheServedAudio(t *testing.T) {
	served := seedReadyTrack(t)
	store := newRecordingStore(served.originalRef)
	served.ledger.FailWrites(errors.New("database unavailable"))
	svc := service.NewAcquireTrackAudioService(served.ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	if err := svc.ExecuteReplace(context.Background(), served.userID, served.trackID); err == nil {
		t.Fatal("ExecuteReplace succeeded, want the failed commit")
	}

	if !store.holds(served.originalRef) || len(store.objects) != 1 {
		t.Errorf("objects = %v, want only the served %q", store.objects, served.originalRef)
	}
}

func TestStoreRollback_OfAScopedStoreDeletesItAndKeepsTheServedAudio(t *testing.T) {
	track := pending("Halo", "Beyonce", "X")
	canonical := refOf(track, "")
	ledger, store := acquisitiontest.NewLedger(track, servedBy(track, canonical)), newRecordingStore(canonical)
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if !store.holds(canonical) || len(store.objects) != 1 {
		t.Errorf("objects = %v, want only the served %q: the scoped write is rolled back", store.objects, canonical)
	}
}

func TestStoreRollback_RetriesATransientDeleteFailure(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store := acquisitiontest.NewLedger(track), newRecordingStore()
	store.failDeletes = 2
	ledger.FailWrites(errors.New("database unavailable"))

	if err := (storing{t, ledger, store, track}).execute(); err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}

	if len(store.objects) != 0 || store.deleteCalls != 3 {
		t.Errorf("objects = %v after %d delete calls, want the audio deleted on the third", store.objects, store.deleteCalls)
	}
}

func TestStoreRollback_SurfacesAPermanentDeleteFailureAndQueuesTheOrphan(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store, orphans := acquisitiontest.NewLedger(track), newRecordingStore(), &orphanLog{}
	store.failDeletes = 1 << 30
	ledger.FailWrites(errors.New("database unavailable"))

	err := (storing{t, ledger, store, track}).execute(service.WithAcquireOrphanQueue(orphans))

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
	ledger := &refUsageLedger{Ledger: acquisitiontest.NewLedger(track), usage: func(call int) (bool, error) { return call > 1, nil }}
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
	ledger := &refUsageLedger{Ledger: acquisitiontest.NewLedger(track), usage: func(call int) (bool, error) {
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
	ledger := cancellingCompleteLedger{Ledger: acquisitiontest.NewLedger(track), cancel: cancel}
	store := newRecordingStore()
	svc := service.NewAcquireTrackAudioService(ledger, sourceFor(track), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	if err := svc.Execute(ctx, track.UserId, track.ID); err == nil {
		t.Fatal("Execute succeeded, want the cancelled commit to fail")
	}

	if len(store.objects) != 0 {
		t.Errorf("objects = %v, want the audio deleted although the job context was cancelled", store.objects)
	}
}
