package service

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type ctxRecordingPublisher struct {
	mu     sync.Mutex
	onLive map[events.EventType]bool
}

func newCtxRecordingPublisher() *ctxRecordingPublisher {
	return &ctxRecordingPublisher{onLive: make(map[events.EventType]bool)}
}

func (p *ctxRecordingPublisher) Publish(ctx context.Context, _ shared.UserId, eventType events.EventType, _ map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onLive[eventType] = ctx.Err() == nil
}

func (p *ctxRecordingPublisher) publishedOnLiveCtx(eventType events.EventType) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.onLive[eventType]
}

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
		catalogbridge.NewTrackLedger(liveCtxTrackRepository{repo}), NewSourceRegistry(source), newFakeAudioStore(),
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

func TestExecuteReplace_KeepsSupersededAudioATwinTrackStillServes(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	twinId := seedTwinTrack(t, repo, userId, originalRef)
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

	execErr := svc.Execute(context.Background(), userId, track.ID)

	if execErr != nil {
		t.Fatalf("expected nil for already-ready track with existing audio, got %v", execErr)
	}

	updated := storedTrack(t, repo, track)
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := storedTrack(t, repo, track)
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := storedTrack(t, repo, track)
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
		catalogbridge.NewTrackLedger(repo),
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
			svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), opts...)

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
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			searcher := &fakeAudioSearcher{searchResults: tt.candidates}
			svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

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
		catalogbridge.NewTrackLedger(newFakeTrackRepository()), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
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
	if ac.Track.ISRC != "" {
		t.Errorf("track isrc = %q, want a resolver guess kept off the track", ac.Track.ISRC)
	}
	if ac.Identity.ISRC != "USUM71922973" {
		t.Errorf("identity isrc = %q, want the resolved one", ac.Identity.ISRC)
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

func TestResolveIdentity_WithResolvedMBIDAsksIdentifierForItsCluster(t *testing.T) {
	identifier := &stubIdentifier{cluster: []string{"acoustid-1"}}
	svc := NewAcquireTrackAudioService(
		catalogbridge.NewTrackLedger(newFakeTrackRepository()), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mbid-1"}}),
		WithAudioIdentifier(identifier),
	)
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A"}}

	svc.resolveIdentity(context.Background(), ac)

	if len(ac.Identity.AcoustIDs) != 1 || ac.Identity.AcoustIDs[0] != "acoustid-1" {
		t.Errorf("identity.AcoustIDs = %v, want the identifier's cluster recorded", ac.Identity.AcoustIDs)
	}
}

func TestResolveIdentity_WithoutAnIdentifierNeverAsksForACluster(t *testing.T) {
	svc := NewAcquireTrackAudioService(
		catalogbridge.NewTrackLedger(newFakeTrackRepository()), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mbid-1"}}),
	)
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A"}}

	svc.resolveIdentity(context.Background(), ac)

	if len(ac.Identity.AcoustIDs) != 0 {
		t.Errorf("identity.AcoustIDs = %v, want none without a configured identifier", ac.Identity.AcoustIDs)
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

	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

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

	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

	snapshot, err := svc.ledger.Get(context.Background(), track.ID, userId)
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot: %v, %v", snapshot, err)
	}
	proceed, reconcileErr := svc.reconcileForReacquire(context.Background(), snapshot)

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

func (r *committingTrackRepo) committed(id domain.TrackId, userId shared.UserId) domain.Track {
	return r.rows[id.String()+":"+userId.String()]
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
	return fetchedAt(path, os.WriteFile(path, []byte("new bytes"), 0o600))
}

func trackRefOf(track *domain.Track) TrackRef {
	return TrackRef{
		ID:     track.ID.String(),
		UserID: track.UserId.String(),
		Title:  track.Title,
		Artist: track.Artist,
		Album:  track.Album,
	}
}

func seedReadyTrack(t *testing.T) (*committingTrackRepo, *bytesAudioStore, shared.UserId, domain.TrackId, string) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	originalRef := BuildAudioRef(trackRefOf(track), "audio.mp3")
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

	if err := svc.Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	row := repo.committed(track.ID, userId)
	want := BuildAudioRef(trackRefOf(track), "audio.mp3")
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
		t.Fatal("expected the replace to fail with no candidates")
	}

	updated := storedTrack(t, repo, track)
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	updated := storedTrack(t, repo, track)
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", updated.AcquisitionStatus)
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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), newFakeAudioStore())

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
	got, ok := repo.tracks[track.ID.String()+":"+track.UserId.String()]
	if !ok || got == nil {
		t.Fatal("track missing from repository")
	}
	return got
}

func markReadyWith(ref string) func(*domain.Track) error {
	return func(tr *domain.Track) error { return tr.MarkReady(ref) }
}

func TestAcquire_StaleFailureDoesNotOverwriteReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/good.opus"))
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("status = %v, want ready: a stale failure overwrote a completed acquisition", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/good.opus" {
		t.Errorf("AudioRef = %v, want the concurrent success's audio kept", got.AudioRef)
	}
}

func TestAcquire_DuplicateFailureKeepsOriginalReason(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, func(tr *domain.Track) error {
		return tr.MarkFailed(string(domain.FailureAcquisitionInterrupted))
	})
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

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
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.AcquisitionStatus)
	}
}

func TestResolveIdentity_QueryCarriesTheTracksDuration(t *testing.T) {
	stub := &stubResolver{}
	svc := serviceWithResolver(stub)
	ac := &AcquisitionContext{Track: TrackRef{Title: "Circles", Artist: "Post Malone", Duration: 215}}

	svc.resolveIdentity(context.Background(), ac)

	if len(stub.queries) != 1 || stub.queries[0].Duration != 215 {
		t.Errorf("queries = %+v, want one carrying Duration 215", stub.queries)
	}
}

type fakeRejectionStore struct {
	active    []string
	activeErr error
	recordErr error
	recorded  []ports.CandidateRejectionRecord
	since     time.Time
}

func (f *fakeRejectionStore) Record(_ context.Context, recs []ports.CandidateRejectionRecord) error {
	f.recorded = append(f.recorded, recs...)
	return f.recordErr
}

func (f *fakeRejectionStore) ActiveKeys(_ context.Context, _ string, since time.Time) ([]string, error) {
	f.since = since
	return f.active, f.activeErr
}

const (
	drmURL   = "https://soundcloud.com/bran-van-3000/drinking-in-l-a-3"
	otherURL = "https://youtube.com/watch?v=dQw4w9WgXcQ"
)

func namedCandidate(url string) ports.AudioCandidate {
	return ports.AudioCandidate{Title: "Lil Tecca - Fell In Love", URL: url, Source: "fake"}
}

func retryWith(t *testing.T, store ports.RejectionStore, urls ...string) (*fakeAudioSearcher, *fakeTrackRepository) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(), WithRejectionStore(store))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher, repo
}

func TestExecute_RetrySkipsAURLRejectedAsDRMTenDaysAgo(t *testing.T) {
	store := &fakeRejectionStore{active: []string{sourceKey(drmURL)}}

	searcher, _ := retryWith(t, store, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
	if got := time.Since(store.since); got < 29*24*time.Hour || got > 31*24*time.Hour {
		t.Errorf("active keys asked since %v ago, want about 30 days", got)
	}
}

func TestExecute_RetryAttemptsAPriorRejectionWhenItIsTheOnlyCandidate(t *testing.T) {
	store := &fakeRejectionStore{active: []string{sourceKey(drmURL)}}

	searcher, _ := retryWith(t, store, drmURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != drmURL {
		t.Errorf("download attempts = %v, want the only candidate %q", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_RecordsDurationRejectionsAndNeverDownloadOnes(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	if err := track.SetDuration(200); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	short := namedCandidate("https://youtube.com/watch?v=shortshort1")
	short.Duration = 30
	failing := namedCandidate(otherURL)
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{short, failing}, downloadErr: errors.New("boom")}
	store := &fakeRejectionStore{}
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(), WithRejectionStore(store))

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(store.recorded) != 1 || store.recorded[0].Reason != "duration" || store.recorded[0].SourceKey != sourceKey(short.URL) {
		t.Errorf("recorded = %+v, want one duration rejection for %q", store.recorded, short.URL)
	}
}

func TestExecute_FailingRejectionStoreNeverChangesTheOutcome(t *testing.T) {
	store := &fakeRejectionStore{activeErr: errors.New("db down"), recordErr: errors.New("db down")}

	searcher, repo := retryWith(t, store, otherURL)

	if len(searcher.downloadURLs) != 1 {
		t.Errorf("download attempts = %v, want the candidate still tried", searcher.downloadURLs)
	}
	for _, track := range repo.tracks {
		if track == nil {
			t.Fatal("nil track in repository")
		}
		if track.AcquisitionStatus != domain.AcquisitionFailed {
			t.Errorf("status = %v, want failed from the download, not the store", track.AcquisitionStatus)
		}
	}
}

func TestExecuteReplace_ExcludesStoredKeysAndRejectedSourceKeys(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")
	track.RejectedSourceKeys = []string{sourceKey("https://youtube.com/watch?v=earlier0000")}
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{"https://youtube.com/watch?v=earlier0000", drmURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	rejections := &fakeRejectionStore{active: []string{sourceKey(drmURL)}}
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store, WithRejectionStore(rejections))

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
}

func transientSearchFailure() *fakeAudioSearcher {
	return &fakeAudioSearcher{searchErr: &acqdomain.SourceUnavailableError{Source: "fake", Err: errors.New("http error 503")}}
}

func executeOnAttempt(t *testing.T, searcher *fakeAudioSearcher, attempt int) (*domain.Track, *recordingPublisher, error) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	pub := newRecordingPublisher()
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(), WithAcquireEvents(pub))
	ctx := withJobAttempt(withSchedulerOwnedJobContext(context.Background()), attempt)

	execErr := svc.Execute(ctx, userId, track.ID)

	return storedTrack(t, repo, track), pub, execErr
}

func TestExecute_TransientFailureBelowTheAttemptCap_ReturnsRetryableAndLeavesTheTrackPending(t *testing.T) {
	for attempt := 1; attempt < maxAcquisitionAttempts; attempt++ {
		stored, pub, execErr := executeOnAttempt(t, transientSearchFailure(), attempt)
		if stored == nil || pub == nil {
			t.Fatal("no stored track or publisher")
		}

		if !errors.Is(execErr, ErrAcquisitionRetryable) {
			t.Errorf("attempt %d: err = %v, want ErrAcquisitionRetryable", attempt, execErr)
		}
		if stored.AcquisitionStatus != domain.AcquisitionPending {
			t.Errorf("attempt %d: status = %v, want pending", attempt, stored.AcquisitionStatus)
		}
		if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
			t.Errorf("attempt %d: failed publishes = %d, want 0", attempt, got)
		}
	}
}

func TestExecute_TransientFailureOnTheLastAttempt_SettlesFailedAsSourceUnavailable(t *testing.T) {
	stored, pub, execErr := executeOnAttempt(t, transientSearchFailure(), maxAcquisitionAttempts)
	if stored == nil || pub == nil {
		t.Fatal("no stored track or publisher")
	}

	if errors.Is(execErr, ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", execErr)
	}
	if stored.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status = %v, want failed", stored.AcquisitionStatus)
	}
	if got := deref(stored.FailureReason); got != string(domain.FailureSourceUnavailable) {
		t.Errorf("failure reason = %q, want %q", got, domain.FailureSourceUnavailable)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 1 {
		t.Errorf("failed publishes = %d, want 1", got)
	}
}

func TestExecute_PermanentFailureOnTheFirstAttempt_SettlesFailedAsNoMatch(t *testing.T) {
	stored, _, execErr := executeOnAttempt(t, &fakeAudioSearcher{}, 1)
	if stored == nil {
		t.Fatal("no stored track")
	}

	if errors.Is(execErr, ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", execErr)
	}
	if stored.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status = %v, want failed", stored.AcquisitionStatus)
	}
	if got := deref(stored.FailureReason); !strings.HasPrefix(got, string(domain.FailureNoMatchFound)) {
		t.Errorf("failure reason = %q, want prefix %q", got, domain.FailureNoMatchFound)
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

func acquireUnknownTopicWithFloor(t *testing.T, opts ...func(*AcquireTrackAudioService)) (*domain.Track, error) {
	t.Helper()
	return acquireUnknownTopicIn(t, context.Background(), opts...)
}

func acquireUnknownTopicIn(t *testing.T, ctx context.Context, opts ...func(*AcquireTrackAudioService)) (*domain.Track, error) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	seconds := 200.0
	track.DurationSeconds = &seconds
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(unknownTopicSource{}),
		&bytesAudioStore{objects: map[string]string{}},
		append([]func(*AcquireTrackAudioService){
			WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding"}}),
			WithAudioIdentifier(&stubIdentifier{}),
		}, opts...)...)
	err = svc.Execute(ctx, userId, track.ID)
	return storedTrack(t, repo, track), err
}

func TestExecute_ReportsStageTimesSourceFormatAndStoredFile(t *testing.T) {
	rep := &recordingReporter{}

	if _, err := acquireUnknownTopicIn(t, withJobReporter(context.Background(), rep)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, stage := range []string{"identity", "search", "select", "download", "download.full", "download.identify", "tag", "store", "update_track"} {
		if _, ok := rep.timings[stage]; !ok {
			t.Errorf("no timing for %q; got %v", stage, rep.timings)
		}
	}
	if _, ok := rep.timings["download.preview"]; ok {
		t.Errorf("download.preview timed, but this source has no preview")
	}
	if want := (ports.SourceFormat{ID: "140", Codec: "mp4a.40.2", Kbps: 128}); rep.format != want {
		t.Errorf("source format = %+v, want %+v", rep.format, want)
	}
	if rep.file.Bytes != int64(len("new bytes")) || rep.file.Codec != "mp3" {
		t.Errorf("stored file = %+v, want the uploaded mp3's size and codec", rep.file)
	}
}

func TestExecute_FloorAboveTheCandidatesConfidenceFailsAsNoConfidentMatch(t *testing.T) {
	track, err := acquireUnknownTopicWithFloor(t, WithAcquireConfidenceFloor(0.9))
	if track == nil {
		t.Fatal("no stored track")
	}

	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", track.AcquisitionStatus)
	}
}

func TestExecute_DefaultFloorStoresTheSameCandidate(t *testing.T) {
	track, err := acquireUnknownTopicWithFloor(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if track.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want ready", track.AcquisitionStatus)
	}
}

func TestSettleContext_SurvivesParentCancelWithinBudget(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	settle, cancel := settleContext(parent)
	defer cancel()

	if settle.Err() != nil {
		t.Fatalf("settle context should outlive a cancelled parent, got %v", settle.Err())
	}
	deadline, ok := settle.Deadline()
	if !ok {
		t.Fatal("settle context must carry a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > settleBudget {
		t.Fatalf("deadline should fall within %v, got %v remaining", settleBudget, remaining)
	}
}

type m4aAudioSource struct{ newAudioSource }

func (m4aAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.m4a")
	return fetchedAt(path, os.WriteFile(path, []byte("aac bytes"), 0o600))
}

func TestExecuteReplace_AnM4AReplacesAnOldMP3AndDropsIt(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(m4aAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	row := repo.committed(trackId, userId)
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
		Title:    "Blinding Lights",
		URL:      "https://music.youtube.com/watch?v=artTrackAAA",
		Channel:  "YouTube Music Art Track",
		Resolved: true,
	}}, nil
}

func (*brokenArtTrackSource) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("yt-dlp download: exit status 1")
}

func TestExecuteReplace_AFailedFirstTierDownloadFallsBackToTheTextSearch(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	first := &brokenArtTrackSource{}
	registry := NewSourceRegistry(first).WithFallback(newAudioSource{})
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), registry, store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the fallback candidate to succeed", err)
	}
	row := repo.committed(trackId, userId)
	if row.AudioRef == nil || store.objects[*row.AudioRef] != "new bytes" {
		t.Fatalf("stored audio = %v, want the fallback source's audio", row.AudioRef)
	}
	if first.finds != 1 {
		t.Errorf("first tier searched %d times, want 1 (the retry asks only the fallback)", first.finds)
	}
}

func TestExecuteReplace_NoFallbackRetryWhenTheFallbackAlreadyRan(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	unresolvedBroken := &stubSource{name: "ytdlp", found: []ports.AudioCandidate{candidate("https://www.youtube.com/watch?v=brokenAAAAA")}, fetchErr: errors.New("exit status 1")}
	first := &stubSource{name: "ytmsearch"}
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(first).WithFallback(unresolvedBroken), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("ExecuteReplace = nil, want the download failure")
	}
	if unresolvedBroken.finds != 1 {
		t.Errorf("fallback searched %d times, want exactly 1", unresolvedBroken.finds)
	}
}

func TestConfigureReplaceExclusion_UnknownSourceRecordsTheStoredLengthNotARank(t *testing.T) {
	stored, oldRef := 112.0, "audio/old.m4a"
	track := &ports.TrackSnapshot{AudioRef: &oldRef, StoredDurationSeconds: &stored}
	ac := &AcquisitionContext{}

	configureReplaceExclusion(context.Background(), ac, track, shared.NewTrackId())

	artTrack := ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=TB0z2HVFxEE", Duration: 112}
	upload := ports.AudioCandidate{URL: "https://www.youtube.com/watch?v=fV3KcFFVZJ8", Duration: 129}
	if !ac.Replace.excludesCandidate(artTrack) {
		t.Error("the candidate matching the stored file's length must be excluded")
	}
	if ac.Replace.excludesCandidate(upload) {
		t.Error("a 129 s upload is not the 112 s stored file and must stay eligible")
	}
}

func TestConfigureReplaceExclusion_KnownSourceIgnoresLength(t *testing.T) {
	stored, oldRef := 112.0, "audio/old.m4a"
	source := "https://www.youtube.com/watch?v=known"
	track := &ports.TrackSnapshot{AudioRef: &oldRef, AudioSourceURL: &source, StoredDurationSeconds: &stored}
	ac := &AcquisitionContext{}

	configureReplaceExclusion(context.Background(), ac, track, shared.NewTrackId())

	if ac.Replace.excludesCandidate(ports.AudioCandidate{URL: "https://example.com/other", Duration: 112}) {
		t.Error("with a known source only the source key excludes")
	}
	if !ac.Replace.excludesCandidate(ports.AudioCandidate{URL: source, Duration: 300}) {
		t.Error("the known source must be excluded")
	}
}

func TestExecuteReplace_FailsCleanlyWhenNoFactIdentifiesTheCurrentFile(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	key := trackId.String() + ":" + userId.String()
	row := repo.rows[key]
	row.AudioSourceURL = nil
	row.DurationSeconds = nil
	repo.rows[key] = row
	source := &stubSource{name: "ytmsearch", found: []ports.AudioCandidate{candidate("https://www.youtube.com/watch?v=otherAAAAAA")}}
	svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), store)

	err := svc.ExecuteReplace(context.Background(), userId, trackId)

	if !errors.Is(err, ErrReplaceFactMissing) {
		t.Fatalf("ExecuteReplace = %v, want ErrReplaceFactMissing", err)
	}
	if errors.Is(err, ErrAcquisitionRetryable) {
		t.Error("a missing fact is not retryable")
	}
	if source.finds != 0 {
		t.Errorf("sources searched %d times, want 0 (the replace must not run blind)", source.finds)
	}
	if got := store.objects[originalRef]; got != "original bytes" || len(store.objects) != 1 {
		t.Errorf("store = %v, want only the original audio untouched", store.objects)
	}
	if got := repo.rows[key].AudioRef; got == nil || *got != originalRef {
		t.Errorf("track audio ref = %v, want %q kept", got, originalRef)
	}
}

func TestBuildTrackRef_CarriesTheCatalogPins(t *testing.T) {
	mbid := "b1a9c0e9-d987-4042-ae91-78d6a3267d69"
	explicit := true
	sources := []ports.CatalogSource{{Provider: "ytmusic", ExternalID: "dQw4w9WgXcQ"}, {Provider: "soundcloud", ExternalID: "123"}}

	ref := buildTrackRef(&ports.TrackSnapshot{CatalogMBID: &mbid, CatalogSources: sources, CatalogExplicit: &explicit})

	if ref.CatalogMBID != mbid {
		t.Errorf("CatalogMBID = %q, want %q", ref.CatalogMBID, mbid)
	}
	if len(ref.CatalogSources) != 2 || ref.CatalogSources[0] != sources[0] || ref.CatalogSources[1] != sources[1] {
		t.Errorf("CatalogSources = %v, want %v", ref.CatalogSources, sources)
	}
	if ref.CatalogExplicit == nil || !*ref.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v, want true", ref.CatalogExplicit)
	}

	if bare := buildTrackRef(&ports.TrackSnapshot{}); bare.CatalogMBID != "" || bare.CatalogSources != nil {
		t.Errorf("unpinned track carries pins: %+v", bare)
	}
}
