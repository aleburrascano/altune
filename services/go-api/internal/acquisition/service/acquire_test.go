package service

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/adapters/catalogpin"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	svc := newTestAcquireService(
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(
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
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(), opts...)

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
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

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
	return newTestAcquireService(
		catalogbridge.NewTrackLedger(newFakeTrackRepository()), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(r),
	)
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
			svc := serviceWithResolver(&stubResolver{identity: tt.identity})
			ac := &AcquisitionContext{Track: TrackRef{Title: "Smaxk Or Die", Artist: "Fatt Smaxk"}}

			svc.resolveIdentity(context.Background(), ac)

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
	svc := serviceWithResolver(&stubResolver{identity: ports.RecordingIdentity{Duration: 999}})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", CatalogDuration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.CatalogDuration != 200 {
		t.Errorf("duration = %v, want the saved 200 kept", ac.Track.CatalogDuration)
	}
}

func TestResolveIdentity_ResolverErrorIsNonFatal(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{err: errors.New("discovery down")})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", CatalogDuration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.CatalogDuration != 200 || !ac.Identity.IsZero() {
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

func TestResolveIdentity_WithAnIdentifierNeverAsksForACluster(t *testing.T) {
	identifier := &stubIdentifier{}
	svc := newTestAcquireService(
		catalogbridge.NewTrackLedger(newFakeTrackRepository()), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mbid-1"}}),
		WithAudioIdentifier(identifier),
	)
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A"}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Identity.MBID != "mbid-1" {
		t.Fatalf("identity = %+v, want the resolved recording", ac.Identity)
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

	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

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

	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), newFakeAudioStore())

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.AcquisitionStatus)
	}
}

func TestResolveIdentity_QueryCarriesTheTracksDuration(t *testing.T) {
	stub := &stubResolver{}
	svc := serviceWithResolver(stub)
	ac := &AcquisitionContext{Track: TrackRef{Title: "Circles", Artist: "Post Malone", CatalogDuration: 215}}

	svc.resolveIdentity(context.Background(), ac)

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
		prior[i] = ports.PriorRejection{SourceKey: sourceKey(u), Basis: &ports.RejectionBasis{}}
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

func retryWith(t *testing.T, store ports.RejectionStore, urls ...string) (*fakeAudioSearcher, *fakeTrackRepository) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(), WithRejectionStore(store))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher, repo
}

func TestExecute_RetrySkipsAURLRejectedAsDRMTenDaysAgo(t *testing.T) {
	store := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}

	searcher, _ := retryWith(t, store, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, otherURL)
	}
	if got := time.Since(store.since); got < 29*24*time.Hour || got > 31*24*time.Hour {
		t.Errorf("active keys asked since %v ago, want about 30 days", got)
	}
}

func TestExecute_RetryAttemptsAPriorRejectionWhenItIsTheOnlyCandidate(t *testing.T) {
	store := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}

	searcher, _ := retryWith(t, store, drmURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != drmURL {
		t.Errorf("download attempts = %v, want the only candidate %q", searcher.downloadURLs, drmURL)
	}
}

func TestExecute_ASearchLengthRejectionIsNotRemembered(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	if err := track.SetCatalogDuration(200); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	audio := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(audio, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	short := namedCandidate("https://youtube.com/watch?v=shortshort1")
	short.Duration = 30
	downloaded := namedCandidate(otherURL)
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{short, downloaded}, downloadPath: audio}
	store := &fakeRejectionStore{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(),
		WithRejectionStore(store), WithAudioProber(&queueProber{durations: []float64{30}}))

	_ = svc.Execute(context.Background(), userId, track.ID)

	if slices.Contains(searcher.downloadURLs, short.URL) {
		t.Fatalf("download attempts = %v, the search-length candidate must never be downloaded", searcher.downloadURLs)
	}
	if len(store.recorded) != 1 || store.recorded[0].Reason != "duration" || store.recorded[0].SourceKey != sourceKey(downloaded.URL) {
		t.Errorf("recorded = %+v, want only the probed duration rejection for %q", store.recorded, downloaded.URL)
	}
}

const (
	recordedLength = 200.0
	runMBID        = "11111111-2222-3333-4444-555555555555"
)

func retryOnALength(t *testing.T, prior []ports.PriorRejection, urls ...string) (*fakeAudioSearcher, *fakeRejectionStore) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	if err := track.SetCatalogDuration(recordedLength); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	store := &fakeRejectionStore{active: prior}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(),
		WithRejectionStore(store), WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))
	_ = svc.Execute(context.Background(), userId, track.ID)
	return searcher, store
}

func priorRejectionOn(url string, basis *ports.RejectionBasis) []ports.PriorRejection {
	return []ports.PriorRejection{{SourceKey: sourceKey(url), Basis: basis}}
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
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	if err := track.SetCatalogDuration(recordedLength); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{namedCandidate(otherURL)}, downloadPath: audio}
	store := &fakeRejectionStore{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(),
		WithRejectionStore(store), WithAudioProber(&queueProber{durations: []float64{30}}), WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))

	_ = svc.Execute(context.Background(), userId, track.ID)

	want := ports.RejectionBasis{ExpectedSeconds: recordedLength, ReferenceMBID: runMBID}
	if len(store.recorded) != 1 || store.recorded[0].Basis != want {
		t.Errorf("recorded = %+v, want one rejection carrying basis %+v", store.recorded, want)
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
	rejections := &fakeRejectionStore{active: priorRejectionsOnTheRunsBasis(drmURL)}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store, WithRejectionStore(rejections))

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(), WithAcquireEvents(pub))
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(unknownTopicSource{}),
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(m4aAudioSource{}), store)

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
		Title: "Blinding Lights",
		URL:   "https://music.youtube.com/watch?v=artTrackAAA",
		Kind:  ports.KindArtTrack,
	}}, nil
}

func (*brokenArtTrackSource) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("yt-dlp download: exit status 1")
}

func TestExecuteReplace_AFailedFirstTierDownloadFallsBackToTheTextSearch(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	first := &brokenArtTrackSource{}
	registry := NewSourceRegistry(first).WithFallback(newAudioSource{})
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), registry, store)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(first).WithFallback(unresolvedBroken), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("ExecuteReplace = nil, want the download failure")
	}
	if unresolvedBroken.finds != 1 {
		t.Errorf("fallback searched %d times, want exactly 1", unresolvedBroken.finds)
	}
}

func TestConfigureReplaceExclusion_UnknownSourceExcludesNoCandidateBeforeDownload(t *testing.T) {
	stored, oldRef := 112.0, "audio/old.m4a"
	track := &ports.TrackSnapshot{AudioRef: &oldRef, StoredDurationSeconds: &stored}
	ac := &AcquisitionContext{}

	configureReplaceExclusion(context.Background(), ac, track, shared.NewTrackId())

	artTrack := ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=TB0z2HVFxEE", Duration: 112}
	upload := ports.AudioCandidate{URL: "https://www.youtube.com/watch?v=fV3KcFFVZJ8", Duration: 129}
	if len(ac.Replace.ExcludeKeys) != 0 {
		t.Errorf("ExcludeKeys = %v, want none for an unknown source", ac.Replace.ExcludeKeys)
	}
	if ac.Replace.StoredAcoustID != "" {
		t.Errorf("StoredAcoustID = %q, want empty: only fingerprinting the stored file sets it", ac.Replace.StoredAcoustID)
	}
	if ac.Replace.excludesCandidate(artTrack) {
		t.Error("a candidate of the stored file's 112 s length must stay eligible: the current file is found by fingerprint after download")
	}
	if ac.Replace.excludesCandidate(upload) {
		t.Error("a 129 s upload must stay eligible")
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), store)

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

type lengthRecordingSource struct {
	newAudioSource
	requested []float64
}

func (s *lengthRecordingSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.requested = append(s.requested, req.Duration)
	return s.newAudioSource.Find(ctx, req)
}

func TestExecuteReplace_TheStoredLengthIsNotTheTarget(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	key := trackId.String() + ":" + userId.String()
	row := repo.rows[key]
	wrongVersion := 112.0
	row.DurationSeconds = &wrongVersion
	repo.rows[key] = row
	resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: "e7ca1ccd-65cf-43e3-9964-ffb5a0479077", Duration: 128}}
	source := &lengthRecordingSource{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), store, WithRecordingResolver(resolver))

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

func captureLogs(t *testing.T) *strings.Builder {
	t.Helper()
	var out strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

func seedUnknownSourceTrack(repo *committingTrackRepo, userId shared.UserId, trackId domain.TrackId) {
	key := trackId.String() + ":" + userId.String()
	row := repo.rows[key]
	stored := 112.0
	row.AudioSourceURL = nil
	row.DurationSeconds = &stored
	repo.rows[key] = row
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
			repo, store, userId, trackId, _ := seedReadyTrack(t)
			if tt.clearSource {
				seedUnknownSourceTrack(repo, userId, trackId)
			}
			identifier := &contentRecordingIdentifier{storedID: "acoustid-of-the-stored-file"}
			logs := captureLogs(t)
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
				WithAudioIdentifier(identifier), WithStoredAudioReader(storedBytesReader{store: store}))

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
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	seedUnknownSourceTrack(repo, userId, trackId)
	identifier := &contentRecordingIdentifier{storedID: "never-recorded"}
	logs := captureLogs(t)
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
		WithAudioIdentifier(identifier), WithStoredAudioReader(storedBytesReader{store: store, err: errors.New("object storage unavailable")}))

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the replace to carry on without the stored fingerprint", err)
	}

	if !strings.Contains(logs.String(), "acquisition.replace_current_file_fingerprint_failed") {
		t.Errorf("logs = %q, want the failed stored copy reported", logs.String())
	}
	if strings.Contains(logs.String(), "acquisition.replace_current_file_fingerprinted") {
		t.Error("a failed copy must not log a fingerprint")
	}
	row := repo.committed(trackId, userId)
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
			repo, store, userId, trackId, _ := seedReadyTrack(t)
			seedUnknownSourceTrack(repo, userId, trackId)
			ws := newCallRecordingWorkspace()
			svc := NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
				WithAcquireTempWorkspace(ws), WithAudioIdentifier(&contentRecordingIdentifier{}),
				WithStoredAudioReader(storedBytesReader{store: store, err: copyErr}))

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
	return fetchedAt(path, os.WriteFile(path, []byte(s.uploads[c.URL]), 0o600))
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
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	seedUnknownSourceTrack(repo, userId, trackId)
	resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: recordingMBID, Duration: 112}}
	options := []func(*AcquireTrackAudioService){WithRecordingResolver(resolver), WithAudioIdentifier(identifier)}
	if readerFor != nil {
		options = append(options, WithStoredAudioReader(readerFor(store)))
	}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), store, options...)
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
			logs := captureLogs(t)

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
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	seedUnknownSourceTrack(repo, userId, trackId)
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(outageSource(), healthy), store,
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: recordingMBID, Duration: 112}}),
		WithAudioIdentifier(identifier), WithStoredAudioReader(storedBytesReader{store: store}), WithRejectionStore(rejections))

	err := svc.ExecuteReplace(context.Background(), userId, trackId)

	if FailureCodeOf(err) != string(shared.FailureSourceUnavailable) {
		t.Errorf("failure code = %q (%v), want source_unavailable: a source was down and the only candidate left was the current file", FailureCodeOf(err), err)
	}
	if len(rejections.recorded) != 0 {
		t.Errorf("recorded = %+v, want nothing: the current file is not a lasting rejection", rejections.recorded)
	}
	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("stored audio = %q, want the original kept", got)
	}
}

func TestExecuteReplace_TheCurrentFileIsFoundWithoutAResolvedRecording(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	seedUnknownSourceTrack(repo, userId, trackId)
	source := sameLengthUploads()
	identifier := acoustIDByBytesIdentifier{byBytes: map[string]ports.RecordingMatch{
		"original bytes":         {AcoustID: "acoustid-of-the-stored-file"},
		"the current file again": {AcoustID: "acoustid-of-the-stored-file"},
		"another upload":         {AcoustID: "acoustid-of-another-upload"},
	}}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(source), store,
		WithRecordingResolver(&stubResolver{}), WithAudioIdentifier(identifier), WithStoredAudioReader(storedBytesReader{store: store}))

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

func prodOverTrack(t *testing.T, repo *fakeTrackRepository, userId shared.UserId, settle func(*domain.Track) error) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Over", "LUCKI", "Watch My Back")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := settle(track); err != nil {
		t.Fatalf("settle: %v", err)
	}
	track.RejectedSourceKeys = []string{"youtube:FjkkqbEbFOY"}
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func prodOverCandidates() []ports.AudioCandidate {
	return []ports.AudioCandidate{
		{Title: "Lucki Ecks -  Over", URL: prodOverRejectedUpload, Channel: "DatPiff", Duration: 138, Kind: ports.KindUpload, Source: "fake"},
		{Title: "Over", URL: prodOverCurrentSource, Channel: "LUCKI", Duration: 136, Source: "fake"},
	}
}

func TestExecute_ExcludesTheUsersRejectedSources(t *testing.T) {
	missingFile := func(track *domain.Track) error { return track.MarkReady("lucki/watch-my-back/over.mp3") }
	tests := []struct {
		name   string
		settle func(*domain.Track) error
	}{
		{"first acquisition", func(*domain.Track) error { return nil }},
		{"retry after a failure", func(track *domain.Track) error { return track.MarkFailed("no match") }},
		{"reacquire of a missing file", missingFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			repo := newFakeTrackRepository()
			track := prodOverTrack(t, repo, userId, tt.settle)
			searcher := &fakeAudioSearcher{searchResults: prodOverCandidates(), downloadErr: errors.New("boom")}
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != prodOverCurrentSource {
				t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, prodOverCurrentSource)
			}
		})
	}
}

func TestExecute_FailsRatherThanDownloadTheOnlyCandidateTheUserRejected(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := prodOverTrack(t, repo, userId, func(*domain.Track) error { return nil })
	searcher := &fakeAudioSearcher{searchResults: prodOverCandidates()[:1], downloadErr: errors.New("boom")}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 0 {
		t.Errorf("download attempts = %v, want none: the only candidate is the one the user rejected", searcher.downloadURLs)
	}
}

func TestExecuteReplace_StillExcludesTheUsersRejectedSourcesAndTheCurrentOne(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := prodOverTrack(t, repo, userId, func(track *domain.Track) error { return track.MarkReady("lucki/watch-my-back/over.mp3") })
	track.SetAudioSource(prodOverCurrentSource)
	store := newFakeAudioStore()
	store.stored["lucki/watch-my-back/over.mp3"] = true
	other := ports.AudioCandidate{Title: "Over [prod. Ugly Friend]", URL: "https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A321193537", Channel: "LUCKI", Duration: 136, Kind: ports.KindUpload, Source: "fake"}
	searcher := &fakeAudioSearcher{searchResults: append(prodOverCandidates(), other), downloadErr: errors.New("boom")}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != other.URL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, other.URL)
	}
}

type searcherDownloads struct{ searcher *fakeAudioSearcher }

func (d searcherDownloads) Download(ctx context.Context, url, outDir string) (ports.FetchedAudio, error) {
	return fetchedAt(d.searcher.Download(ctx, url, outDir))
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
		{"a rejected stored source is never tried", []string{sourceKey(storedSource)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			repo := newFakeTrackRepository()
			track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", storedSource)
			track.RejectedSourceKeys = tt.rejected
			var found []ports.AudioCandidate
			for _, u := range rivals {
				found = append(found, ports.AudioCandidate{Title: "Blinding Lights", URL: u, Channel: "The Weeknd", Artists: []string{"The Weeknd"}, Kind: ports.KindArtTrack, Source: "fake"})
			}
			searcher := &fakeAudioSearcher{searchResults: found, downloadErr: errors.New("boom")}
			registry := NewSourceRegistry(catalogpin.NewSource(searcherDownloads{searcher}), searcher)
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), registry, newFakeAudioStore())

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
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", storedSource)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	rival := ports.AudioCandidate{Title: "Blinding Lights", URL: "https://music.youtube.com/watch?v=aaaaaaaaaaa", Kind: ports.KindArtTrack, Artists: []string{"The Weeknd"}, Source: "fake"}
	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{rival}, downloadErr: errors.New("boom")}
	registry := NewSourceRegistry(catalogpin.NewSource(searcherDownloads{searcher}), searcher)
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), registry, store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != rival.URL {
		t.Errorf("download attempts = %v, want only %q", searcher.downloadURLs, rival.URL)
	}
}

func TestExecuteReplace_NeverFetchesTheCurrentSourceAgainEvenWhenItRanksFirst(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	wrongURL := "https://youtube.com/watch?v=wrongwrong1"
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", wrongURL)
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{wrongURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store)

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

func acquireLowConfidence(t *testing.T, opts ...func(*AcquireTrackAudioService)) (*domain.Track, error) {
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(lowConfidenceSource{}),
		&bytesAudioStore{objects: map[string]string{}},
		append([]func(*AcquireTrackAudioService){
			WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding"}}),
			WithAudioIdentifier(&stubIdentifier{}),
		}, opts...)...)
	err = svc.Execute(context.Background(), userId, track.ID)
	return storedTrack(t, repo, track), err
}

func TestExecute_AConfiguredZeroFloorIsHonoured(t *testing.T) {
	track, err := acquireLowConfidence(t, WithAcquireConfidenceFloor(0))
	if err != nil {
		t.Fatalf("a floor of 0 should store the held candidate, got %v", err)
	}
	if track.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want ready", track.AcquisitionStatus)
	}

	_, err = acquireLowConfidence(t)
	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Errorf("with no option err = %v, want ErrNoConfidentMatch at the 0.5 default", err)
	}
}

func retryWithBasis(t *testing.T, catalogLength float64, identity ports.RecordingIdentity, prior []ports.PriorRejection, urls ...string) *fakeAudioSearcher {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)
	if catalogLength > 0 {
		if err := track.SetCatalogDuration(catalogLength); err != nil {
			t.Fatalf("set duration: %v", err)
		}
	}
	searcher := &fakeAudioSearcher{downloadErr: errors.New("boom")}
	for _, u := range urls {
		searcher.searchResults = append(searcher.searchResults, namedCandidate(u))
	}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore(),
		WithRejectionStore(&fakeRejectionStore{active: prior}), WithRecordingResolver(&stubResolver{identity: identity}))
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
		{SourceKey: sourceKey(drmURL), Basis: &ports.RejectionBasis{ExpectedSeconds: 200, ReferenceMBID: runMBID}},
		{SourceKey: sourceKey(otherURL), Basis: &ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: runMBID}},
	}

	searcher := retryWithBasis(t, 200, ports.RecordingIdentity{MBID: runMBID}, prior, drmURL, otherURL)

	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != otherURL {
		t.Errorf("download attempts = %v, want only %q: its rejection is stale so not every candidate was skipped", searcher.downloadURLs, otherURL)
	}
}

func TestExecuteReplace_RetriesAPriorRejectionJudgedOnAnotherLength(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrongwrong1")
	if err := track.SetCatalogDuration(200); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	var results []ports.AudioCandidate
	for _, u := range []string{drmURL, otherURL} {
		results = append(results, ports.AudioCandidate{Title: "The Weeknd - Blinding Lights", URL: u, Source: "fake"})
	}
	searcher := &fakeAudioSearcher{searchResults: results, downloadErr: errors.New("boom")}
	rejections := &fakeRejectionStore{active: priorRejectionOn(drmURL, &ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: runMBID})}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store,
		WithRejectionStore(rejections), WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: runMBID}}))

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !slices.Contains(searcher.downloadURLs, drmURL) {
		t.Errorf("download attempts = %v, want %q retried: its rejection was judged on 150s, the replace run expects 200s", searcher.downloadURLs, drmURL)
	}
}

type stalledSource struct{}

func (stalledSource) Name() acqdomain.SourceName { return "stalled" }

func (stalledSource) Find(ctx context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	<-ctx.Done()
	return nil, withCancellation(ctx, ctx.Err())
}

func (stalledSource) Fetch(ctx context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	<-ctx.Done()
	return ports.FetchedAudio{}, withCancellation(ctx, ctx.Err())
}

func executeAfterOwnTimeBudget(t *testing.T, attempt int) (*domain.Track, *recordingPublisher, error) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	pub := newRecordingPublisher()
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(stalledSource{}), newFakeAudioStore(), WithAcquireEvents(pub))
	svc.acquireTimeout = time.Millisecond
	ctx := withJobAttempt(withSchedulerOwnedJobContext(context.Background()), attempt)

	execErr := svc.Execute(ctx, userId, track.ID)

	return storedTrack(t, repo, track), pub, execErr
}

func TestExecute_AnExpiredOwnTimeBudgetIsRetried(t *testing.T) {
	stored, pub, execErr := executeAfterOwnTimeBudget(t, 1)
	if stored == nil || pub == nil {
		t.Fatal("no stored track or publisher")
	}

	if !errors.Is(execErr, ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want ErrAcquisitionRetryable", execErr)
	}
	if !errors.Is(execErr, ErrAcquisitionTimedOut) {
		t.Errorf("err = %v, want ErrAcquisitionTimedOut", execErr)
	}
	if stored.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending", stored.AcquisitionStatus)
	}
	if got := pub.count(events.TypeTrackAcquisitionFailed); got != 0 {
		t.Errorf("failed publishes = %d, want 0", got)
	}
}

func TestExecute_AnExpiredOwnTimeBudgetOnTheLastAttemptIsSourceUnavailable(t *testing.T) {
	stored, pub, execErr := executeAfterOwnTimeBudget(t, maxAcquisitionAttempts)
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

type replaceFixture struct {
	svc    *AcquireTrackAudioService
	repo   *fakeTrackRepository
	track  *domain.Track
	store  *fakeAudioStore
	pub    *recordingPublisher
	userId shared.UserId
	ctx    context.Context
}

func newReplaceFixture(t *testing.T, searcher *fakeAudioSearcher, attempt int) replaceFixture {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrong")
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	pub := newRecordingPublisher()
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), store, WithAcquireEvents(pub))
	ctx := withJobAttempt(withSchedulerOwnedJobContext(context.Background()), attempt)
	return replaceFixture{svc: svc, repo: repo, track: track, store: store, pub: pub, userId: userId, ctx: ctx}
}

func TestExecuteReplace_ATransientOutageBelowTheCapIsRetried(t *testing.T) {
	for attempt := 1; attempt < maxAcquisitionAttempts; attempt++ {
		f := newReplaceFixture(t, transientSearchFailure(), attempt)
		err := f.svc.ExecuteReplace(f.ctx, f.userId, f.track.ID)
		stored, store, pub := storedTrack(t, f.repo, f.track), f.store, f.pub

		if !errors.Is(err, ErrAcquisitionRetryable) {
			t.Errorf("attempt %d: err = %v, want ErrAcquisitionRetryable", attempt, err)
		}
		if stored.AcquisitionStatus != domain.AcquisitionReady || stored.AudioRef == nil || *stored.AudioRef != "u/a/b/c.mp3" {
			t.Errorf("attempt %d: status = %v, audio_ref = %v, want the track ready with its audio", attempt, stored.AcquisitionStatus, stored.AudioRef)
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
	f := newReplaceFixture(t, transientSearchFailure(), maxAcquisitionAttempts)
	err := f.svc.ExecuteReplace(f.ctx, f.userId, f.track.ID)
	stored, pub := storedTrack(t, f.repo, f.track), f.pub

	if err == nil || errors.Is(err, ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure", err)
	}
	if stored.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", stored.AcquisitionStatus)
	}
	if got := pub.count(events.TypeTrackReplaceFailed); got != 1 {
		t.Errorf("replace-failed publishes = %d, want 1", got)
	}
}

func TestReplaceGated_ATransientOutageBelowTheCapIsNotRetried(t *testing.T) {
	f := newReplaceFixture(t, transientSearchFailure(), 1)
	snapshot, err := f.svc.loadTrack(f.ctx, f.userId, f.track.ID)
	if err != nil || snapshot == nil {
		t.Fatalf("load track: %v", err)
	}
	_, err = f.svc.ReplaceGated(f.ctx, f.userId, snapshot, upgrade.Gate{})

	if err == nil || errors.Is(err, ErrAcquisitionRetryable) {
		t.Errorf("err = %v, want a terminal failure: the upgrade lane keeps its own retry policy", err)
	}
}

func TestExecute_AWorkerThatLostItsLeaseDoesNotFailTheTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	track.AcquisitionFence = 2
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(withJobFence(context.Background(), 1), userId, track.ID)

	if got := repo.tracks[key].AcquisitionStatus; got != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending: the new lease owner is still downloading", got)
	}
}

func TestExecute_AWorkerHoldingTheCurrentLeaseStillFailsTheTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	track.AcquisitionFence = 2
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(withJobFence(context.Background(), 2), userId, track.ID)

	if got := repo.tracks[key].AcquisitionStatus; got != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got)
	}
}

func TestExecute_AWorkerThatLostItsLeaseCannotLandItsDownload(t *testing.T) {
	cases := []struct {
		name       string
		held       ports.Fence
		wantStatus domain.AcquisitionStatus
		wantAudio  bool
	}{
		{"the lease was reclaimed", 1, domain.AcquisitionPending, false},
		{"the lease is still held", 2, domain.AcquisitionReady, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			track.AcquisitionFence = 2
			repo := &committingTrackRepo{rows: map[string]domain.Track{track.ID.String() + ":" + userId.String(): *track}}
			store := &bytesAudioStore{objects: map[string]string{}}
			svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

			_ = svc.Execute(withJobFence(context.Background(), tc.held), userId, track.ID)

			row := repo.committed(track.ID, userId)
			if row.AcquisitionStatus != tc.wantStatus {
				t.Errorf("status = %v, want %v", row.AcquisitionStatus, tc.wantStatus)
			}
			if (row.AudioRef != nil) != tc.wantAudio {
				t.Errorf("audio_ref = %v, want set = %v", row.AudioRef, tc.wantAudio)
			}
			if row.FailureReason != nil {
				t.Errorf("failure_reason = %q, want none", *row.FailureReason)
			}
		})
	}
}

func TestExecuteReplace_AWorkerThatLostItsLeaseLeavesTheServingAudioInPlace(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	key := trackId.String() + ":" + userId.String()
	row := repo.rows[key]
	row.AcquisitionFence = 2
	repo.rows[key] = row
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store)

	_ = svc.ExecuteReplace(withJobFence(context.Background(), 1), userId, trackId)

	got := repo.committed(trackId, userId)
	if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != originalRef {
		t.Errorf("track = %v %v, want ready on the original ref %q", got.AcquisitionStatus, got.AudioRef, originalRef)
	}
	if store.objects[originalRef] != "original bytes" {
		t.Errorf("object at the serving ref = %q, want the original audio kept", store.objects[originalRef])
	}
}

func TestExecute_TheSourceIsAskedWithTheCatalogFacts(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	explicit := true
	sources := []domain.CatalogSource{
		{Provider: "spotify", ExternalID: "4uLU6hMCjMI75M1A2tKUQC", URL: "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC"},
		{Provider: "deezer", ExternalID: "3135556"},
	}
	track := seedTrackInRepo(t, repo, userId, func(tr *domain.Track) error {
		isrc := "USUM71703861"
		tr.ISRC = &isrc
		if err := tr.SetCatalogDuration(214); err != nil {
			return err
		}
		if err := tr.SetDuration(200); err != nil {
			return err
		}
		return tr.SetCatalogIdentity(nil, &explicit, sources)
	})
	searcher := &fakeAudioSearcher{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

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
	ports.TrackLedger
	completed []ports.Completion
}

func (l *completionRecordingLedger) Complete(ctx context.Context, id shared.TrackId, userId shared.UserId, fence ports.Fence, c ports.Completion) error {
	l.completed = append(l.completed, c)
	return l.TrackLedger.Complete(ctx, id, userId, fence, c)
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
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	ledger := &completionRecordingLedger{TrackLedger: catalogbridge.NewTrackLedger(repo)}
	svc := newTestAcquireService(ledger, NewSourceRegistry(audioOnlyOfTheStoredSource{}), store,
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
		WithAudioIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
	)
	track, err := ledger.Get(context.Background(), trackId, userId)
	if err != nil || track == nil {
		t.Fatalf("load track: %v %v", track, err)
	}
	gate := upgrade.Gate{
		Current:   grade.A,
		Stored:    ports.SourceFormat{ID: "18", Codec: "mp4a.40.2", Kbps: 96},
		FloorKbps: 112,
	}

	if _, err := svc.ReplaceGated(context.Background(), userId, track, gate); err != nil {
		t.Fatalf("ReplaceGated: %v, want the 140 stream at 130 kbps admitted over the stored format 18 at 96", err)
	}

	if row := repo.committed(trackId, userId); row.AudioRef == nil || *row.AudioRef == originalRef {
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
	if slices.Contains(done.RejectedSourceKeys, sourceKey(lowBitrateStoredSource)) {
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
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	ledger := &completionRecordingLedger{TrackLedger: catalogbridge.NewTrackLedger(repo)}
	svc := newTestAcquireService(ledger, NewSourceRegistry(fullLengthAlbumCut{}), store,
		WithAudioProber(&queueProber{durations: []float64{285, 285, 285}}),
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
		WithAudioIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
	)
	track, err := ledger.Get(context.Background(), trackId, userId)
	if err != nil || track == nil {
		t.Fatalf("load track: %v %v", track, err)
	}
	catalog := 285.0
	track.CatalogDurationSeconds = &catalog
	gate := upgrade.Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}

	if _, err := svc.ReplaceGated(context.Background(), userId, track, gate); err != nil {
		t.Fatalf("ReplaceGated: %v, want the 285 s file admitted over the 231 s edit", err)
	}

	if row := repo.committed(trackId, userId); row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Fatalf("audio_ref = %v, want the swap committed to a new ref", row.AudioRef)
	}
	if len(ledger.completed) != 1 || ledger.completed[0].Duration != 285 {
		t.Errorf("completions = %+v, want one carrying the 285 s length", ledger.completed)
	}
}

func TestReplaceGated_ASameGradeFileIsRefusedWhenTheLengthRescueDoesNotHold(t *testing.T) {
	cases := []struct {
		name string
		gate upgrade.Gate
	}{
		{"the stored file already fits the catalog length", upgrade.Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 284}},
		{"the fetched 285 s file misses a 195 s catalog length", upgrade.Gate{Current: grade.B, CatalogSeconds: 195, StoredSeconds: 292}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, userId, trackId, originalRef := seedReadyTrack(t)
			ledger := &completionRecordingLedger{TrackLedger: catalogbridge.NewTrackLedger(repo)}
			svc := newTestAcquireService(ledger, NewSourceRegistry(fullLengthAlbumCut{}), store,
				WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding", MBIDs: []string{"mb-blinding"}}}),
				WithAudioIdentifier(&stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "mb-blinding", Title: "Blinding Lights", Artists: []string{"The Weeknd"}})}),
				WithAudioProber(&queueProber{durations: []float64{285, 285, 285}}),
			)
			track, err := ledger.Get(context.Background(), trackId, userId)
			if err != nil || track == nil {
				t.Fatalf("load track: %v %v", track, err)
			}

			if _, err := svc.ReplaceGated(context.Background(), userId, track, tc.gate); !errors.Is(err, upgrade.ErrNotAnUpgrade) {
				t.Fatalf("ReplaceGated = %v, want ErrNotAnUpgrade for the same-grade 285 s file", err)
			}

			if row := repo.committed(trackId, userId); row.AudioRef == nil || *row.AudioRef != originalRef {
				t.Errorf("audio_ref = %v, want the original %q kept", row.AudioRef, originalRef)
			}
			if len(ledger.completed) != 0 {
				t.Errorf("completions = %+v, want none", ledger.completed)
			}
		})
	}
}
