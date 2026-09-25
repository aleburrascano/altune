package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// ctxRecordingPublisher remembers whether each event type reached it on a live
// context. A publisher backed by a real transport drops what arrives on a dead
// one, so "was published" alone would prove nothing (#1975).
type ctxRecordingPublisher struct {
	mu     sync.Mutex
	onLive map[string]bool
}

func newCtxRecordingPublisher() *ctxRecordingPublisher {
	return &ctxRecordingPublisher{onLive: make(map[string]bool)}
}

func (p *ctxRecordingPublisher) Publish(ctx context.Context, _ shared.UserId, eventType string, _ map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onLive[eventType] = ctx.Err() == nil
}

func (p *ctxRecordingPublisher) publishedOnLiveCtx(eventType string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.onLive[eventType]
}

// A job whose context ends mid-search must still be settled: against a store
// that rejects a dead context (as a real pool does), both the failure row and
// the failure event have to be issued on a context detached from the job's,
// or the track stays pending until the stale sweep ten minutes later (#1975).
func TestAcquireTrackAudioService_Execute_ContextEndsMidSearch_SettlesOnDetachedContext(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	source := newBlockingSource()
	pub := newCtxRecordingPublisher()
	svc := NewAcquireTrackAudioService(
		liveCtxTrackRepository{repo}, NewSourceRegistry(source), newFakeAudioStore(),
		WithAcquireEvents(pub),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-source.searching
		cancel()
	}()

	_ = svc.Execute(ctx, userId, track.ID)

	settled := repo.tracks[key]
	if settled.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want %v (failed)", settled.AcquisitionStatus, domain.AcquisitionFailed)
	}
	if got := deref(settled.FailureReason); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
	if !pub.publishedOnLiveCtx(events.TypeTrackAcquisitionFailed) {
		t.Errorf("%q was not published on a live context", events.TypeTrackAcquisitionFailed)
	}
}

// seedTwinTrack adds a second Ready track serving the same audio object, the
// row the canonical (metadata-derived) ref produces for equivalent metadata.
func seedTwinTrack(t *testing.T, repo *committingTrackRepo, userId shared.UserId, audioRef string) domain.TrackId {
	t.Helper()
	twin, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new twin track: %v", err)
	}
	if err := twin.MarkReady(audioRef); err != nil {
		t.Fatalf("mark twin ready: %v", err)
	}
	repo.rows[twin.ID.String()+":"+userId.String()] = *twin
	return twin.ID
}

// A committed replace deletes the audio it swapped out — unless a second track
// with equivalent metadata is still serving that same object, in which case the
// delete would leave that track Ready with no file (#1984).
func TestExecuteReplace_KeepsSupersededAudioATwinTrackStillServes(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	twinId := seedTwinTrack(t, repo, userId, originalRef)
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("object at %q = %q, want the audio track %s still serves", originalRef, got, twinId)
	}
	row := repo.committed(trackId, userId)
	if row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Errorf("audio_ref = %v, want the replaced track moved onto its own new ref", row.AudioRef)
	}
}

func TestAcquireTrackAudioService_Execute_TrackNotFound(t *testing.T) {
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	err := svc.Execute(context.Background(), userId, trackId)
	if err != nil {
		t.Fatalf("expected nil for track-not-found (silent no-op), got %v", err)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioExists(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	store.stored[audioRef] = true

	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	execErr := svc.Execute(context.Background(), userId, track.ID)

	if execErr != nil {
		t.Fatalf("expected nil for already-ready track with existing audio, got %v", execErr)
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v (should remain ready)", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
}

func TestAcquireTrackAudioService_Execute_AlreadyReady_AudioMissing(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()

	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus == domain.AcquisitionReady {
		t.Error("track should not remain in 'ready' status when audio file is missing")
	}
}

func TestAcquireTrackAudioService_Execute_FailedStatus_RetriesToAcquire(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	_ = track.MarkFailed("previous failure reason")

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus == domain.AcquisitionPending {
	}
	if updated.FailureReason != nil && *updated.FailureReason == "previous failure reason" {
		t.Error("expected failure reason to change after retry attempt, but it remained the original")
	}
}

func pendingTrack(t *testing.T, repo *fakeTrackRepository, userId shared.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Fell In Love", "Lil Tecca", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func TestExecute_AlwaysSearches(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run")
	}
	if len(searcher.downloadURLs) != 0 {
		t.Errorf("no direct download should occur; got download URLs %v", searcher.downloadURLs)
	}
}

func TestExecute_PublishesStartedEvent(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	pub := &recordingProgressPublisher{}
	svc := NewAcquireTrackAudioService(
		repo,
		fakeRegistry(&fakeAudioSearcher{}),
		newFakeAudioStore(),
		WithAcquireEvents(pub),
	)

	_ = svc.Execute(context.Background(), userId, track.ID)

	var started *recordedProgress
	for i := range pub.events {
		if pub.events[i].typ == "track_acquisition_started" {
			started = &pub.events[i]
			break
		}
	}
	if started == nil {
		t.Fatalf("no track_acquisition_started event published; got %+v", pub.events)
	}
	if started.payload["track_id"] != track.ID.String() {
		t.Fatalf("started track_id = %v, want %s", started.payload["track_id"], track.ID.String())
	}
}

func TestExecute_WithoutConfiguredEventsDoesNotPanic(t *testing.T) {
	for name, opts := range map[string][]func(*AcquireTrackAudioService){
		"no events option":  nil,
		"nil events option": {WithAcquireEvents(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), opts...)

			if err := svc.Execute(context.Background(), userId, track.ID); err == nil {
				t.Fatalf("Execute with no candidates: want error, got nil")
			}
			if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
				t.Fatalf("ExecuteReplace with no candidates: want error, got nil")
			}
		})
	}
}

// Issue #979: the acquisition side must persist a failure_reason whose code the
// catalog failure-message table recognises, so the wire failure_message is the
// specific one for the case instead of the generic fallback.
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
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			searcher := &fakeAudioSearcher{searchResults: tt.candidates}
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			updated := repo.tracks[track.ID.String()+":"+userId.String()]
			if updated == nil || updated.AcquisitionStatus != domain.AcquisitionFailed {
				t.Fatalf("track not marked failed: %+v", updated)
				return
			}
			const want = "Couldn't find this track"
			if got := domain.FailureMessage(updated.FailureReason); got != want {
				t.Errorf("FailureMessage(%q) = %q, want %q", deref(updated.FailureReason), got, want)
			}
		})
	}
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

func serviceWithResolver(r ports.RecordingResolver) *AcquireTrackAudioService {
	return NewAcquireTrackAudioService(
		newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(r),
	)
}

func TestResolveIdentity_SuppliesMissingDuration(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{
		identity: ports.RecordingIdentity{Duration: 181, ISRC: "USUM71922973"},
	})
	ac := &AcquisitionContext{Track: TrackRef{Title: "Smaxk Or Die", Artist: "Fatt Smaxk"}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 181 {
		t.Errorf("duration = %v, want the resolved 181", ac.Track.Duration)
	}
	if ac.Track.ISRC != "USUM71922973" {
		t.Errorf("isrc = %q, want the resolved one", ac.Track.ISRC)
	}
	if ac.Identity.Duration != 181 {
		t.Errorf("identity not recorded on the context: %+v", ac.Identity)
	}
}

func TestResolveIdentity_NeverOverwritesSavedDuration(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{identity: ports.RecordingIdentity{Duration: 999}})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", Duration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 200 {
		t.Errorf("duration = %v, want the saved 200 kept", ac.Track.Duration)
	}
}

func TestResolveIdentity_ResolverErrorIsNonFatal(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{err: errors.New("discovery down")})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", Duration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 200 || !ac.Identity.IsZero() {
		t.Errorf("a resolver failure must leave the track untouched, got %+v", ac.Track)
	}
}

func TestResolveIdentity_PassesTrackMetadataToResolver(t *testing.T) {
	stub := &stubResolver{}
	svc := serviceWithResolver(stub)
	ac := &AcquisitionContext{Track: TrackRef{
		Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding", ISRC: "X",
	}}

	svc.resolveIdentity(context.Background(), ac)

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
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)
}

func TestReconcileForReacquire_ExistsError_PreservesAudioRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	store := newFakeAudioStore()
	store.err = errors.New("transient s3 hiccup")

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	proceed, reconcileErr := svc.reconcileForReacquire(context.Background(), track)

	if reconcileErr == nil {
		t.Fatal("expected reconcileForReacquire to return the exists-check error, got nil")
	}
	if proceed {
		t.Error("expected proceed=false when the exists check errors")
	}
	if track.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want %v (must stay ready on transient error)", track.AcquisitionStatus, domain.AcquisitionReady)
	}
	if track.AudioRef == nil || *track.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q preserved (a transient error is not evidence the file is gone)", track.AudioRef, audioRef)
	}
}

// bytesAudioStore records the bytes behind each ref, so a test can tell the
// original audio apart from a replacement written to the same key.
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

// committingTrackRepo hands out copies so a mutation only becomes visible once
// Update succeeds, the way a database row behaves.
type committingTrackRepo struct {
	rows      map[string]domain.Track
	updateErr error
}

func (r *committingTrackRepo) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	row, ok := r.rows[id.String()+":"+userId.String()]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *committingTrackRepo) Update(_ context.Context, track *domain.Track, _ int) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.rows[track.ID.String()+":"+track.UserId.String()] = *track
	return nil
}

func (r *committingTrackRepo) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	for _, row := range r.rows {
		if row.ID != excludeTrackID && row.AudioRef != nil && *row.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

// committed is the row as last durably written.
func (r *committingTrackRepo) committed(id domain.TrackId, userId shared.UserId) domain.Track {
	return r.rows[id.String()+":"+userId.String()]
}

// newAudioSource offers one fresh candidate and downloads it as "new bytes".
type newAudioSource struct{}

func (newAudioSource) Name() string { return "new-audio" }

func (newAudioSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        "https://www.youtube.com/watch?v=freshAAAAAA",
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
	}}, nil
}

func (newAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (string, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return path, os.WriteFile(path, []byte("new bytes"), 0o600)
}

// seedReadyTrack stores a Ready track whose audio lives at the exact key a
// fresh acquisition of the same metadata would build, i.e. the key a replace
// collides with.
func seedReadyTrack(t *testing.T) (*committingTrackRepo, *bytesAudioStore, shared.UserId, domain.TrackId, string) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	originalRef := BuildAudioRef(buildTrackRef(track), "audio.mp3")
	if err := track.MarkReady(originalRef); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	track.SetAudioSource("https://www.youtube.com/watch?v=currentAAAA")

	repo := &committingTrackRepo{rows: map[string]domain.Track{track.ID.String() + ":" + userId.String(): *track}}
	store := &bytesAudioStore{objects: map[string]string{originalRef: "original bytes"}}
	return repo, store, userId, track.ID, originalRef
}

func TestExecuteReplace_FailedCommitKeepsTheOriginalAudioIntact(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	repo.updateErr = errors.New("transient db error")
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("expected the replace to fail when the track update cannot commit")
	}

	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("object at the serving ref = %q, want the original audio untouched", got)
	}
	if len(store.objects) != 1 {
		t.Errorf("objects = %v, want the failed attempt's staged audio rolled back", store.objects)
	}
	row := repo.committed(trackId, userId)
	if row.AcquisitionStatus != domain.AcquisitionReady || row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it still ready on the original ref", row.AcquisitionStatus, row.AudioRef)
	}
}

func TestExecuteReplace_CommittedSwapPointsAtTheNewAudioAndDropsTheOld(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	row := repo.committed(trackId, userId)
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
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	store.deleteErr = errors.New("object storage unavailable")
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the committed swap reported as success", err)
	}

	row := repo.committed(trackId, userId)
	if row.AudioRef == nil || *row.AudioRef == originalRef || store.objects[*row.AudioRef] != "new bytes" {
		t.Errorf("audio_ref = %v, want the committed ref serving the replacement", row.AudioRef)
	}
}

func TestExecute_FreshAcquisitionKeepsTheCanonicalRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &committingTrackRepo{rows: map[string]domain.Track{track.ID.String() + ":" + userId.String(): *track}}
	store := &bytesAudioStore{objects: map[string]string{}}
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	row := repo.committed(track.ID, userId)
	want := BuildAudioRef(buildTrackRef(track), "audio.mp3")
	if row.AudioRef == nil || *row.AudioRef != want {
		t.Errorf("audio_ref = %v, want the canonical %q", row.AudioRef, want)
	}
}

func readyTrackWithSource(t *testing.T, repo *fakeTrackRepository, userId shared.UserId, ref, source string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady(ref); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	track.SetAudioSource(source)
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func TestExecuteReplace_ExcludesTheStoredSource(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrong")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("replace must run the search pipeline rather than skipping a ready track")
	}
}

func TestExecuteReplace_FailureLeavesTheTrackReady(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrong")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
		t.Fatal("expected the replace to fail with no candidates")
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", updated.AcquisitionStatus)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "u/a/b/c.mp3" {
		t.Errorf("audio_ref = %v, want the original preserved", updated.AudioRef)
	}
	if !store.stored["u/a/b/c.mp3"] {
		t.Error("a failed replace must never delete the audio the user still has")
	}
}

func TestExecute_NonReplaceStillSkipsAReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if searcher.searchCalled {
		t.Error("ordinary acquire must still no-op on a ready track whose audio exists")
	}
}

func TestExecuteReplace_LegacyTrackSkipsTopRankedInsteadOfExcluding(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", updated.AcquisitionStatus)
	}
}

// cancellingSource cancels the job mid-search through the real SourceRegistry.
type cancellingSource struct {
	*fakeAudioSearcher
	cancel func()
}

func (s *cancellingSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.cancel()
	return nil, errors.New("upstream closed")
}

// End to end: the persisted failure_reason of a job cancelled mid-search is
// the cancellation code, not no_match_found.
func TestExecute_CancelledMidSearch_PersistsCancellation(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancellingSource{fakeAudioSearcher: &fakeAudioSearcher{}, cancel: cancel}
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(source), newFakeAudioStore())

	_ = svc.Execute(ctx, userId, track.ID)

	if got := deref(repo.tracks[key].FailureReason); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
}

func seedTrackInRepo(t *testing.T, repo *fakeTrackRepository, userId shared.UserId, settle func(*domain.Track) error) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if settle != nil {
		if err := settle(track); err != nil {
			t.Fatalf("settle track: %v", err)
		}
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func storedTrack(t *testing.T, repo *fakeTrackRepository, track *domain.Track) *domain.Track {
	t.Helper()
	got := repo.tracks[track.ID.String()+":"+track.UserId.String()]
	if got == nil {
		t.Fatal("track missing from repository")
	}
	return got
}

func markReadyWith(ref string) func(*domain.Track) error {
	return func(tr *domain.Track) error { return tr.MarkReady(ref) }
}

// A failure reported by a job whose track a concurrent success already moved
// to ready must not clobber that good audio.
func TestAcquire_StaleFailureDoesNotOverwriteReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/good.opus"))
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("status = %v, want ready: a stale failure overwrote a completed acquisition", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/good.opus" {
		t.Errorf("AudioRef = %v, want the concurrent success's audio kept", got.AudioRef)
	}
}

// A duplicate failure must not overwrite the reason the first one recorded.
func TestAcquire_DuplicateFailureKeepsOriginalReason(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, func(tr *domain.Track) error {
		return tr.MarkFailed(string(domain.FailureAcquisitionInterrupted))
	})
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.FailureReason == nil || *got.FailureReason != string(domain.FailureAcquisitionInterrupted) {
		t.Errorf("FailureReason = %v, want the first failure's reason kept", got.FailureReason)
	}
}

func TestAcquire_FailureOnPendingTrackStillMarksFailed(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.AcquisitionStatus)
	}
}
