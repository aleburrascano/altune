package service

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// captureLogs redirects the default slog logger to a buffer for the duration of
// the test, so a test can assert which fields a structured log line carries.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type inMemoryQueueRepo struct {
	states map[uuid.UUID]*domain.QueueState
}

func newInMemoryQueueRepo() *inMemoryQueueRepo {
	return &inMemoryQueueRepo{states: map[uuid.UUID]*domain.QueueState{}}
}

func (r *inMemoryQueueRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	r.states[state.UserId.UUID()] = state
	return nil
}

func (r *inMemoryQueueRepo) GetForUser(_ context.Context, userId shared.UserId) (*domain.QueueState, error) {
	return r.states[userId.UUID()], nil
}

func (r *inMemoryQueueRepo) DeleteForUser(_ context.Context, userId shared.UserId) error {
	delete(r.states, userId.UUID())
	return nil
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

type fakeNowPlaying struct {
	tracks map[string]*ports.NowPlayingTrack
}

func (f *fakeNowPlaying) Lookup(_ context.Context, _ shared.UserId, trackId string) (*ports.NowPlayingTrack, error) {
	return f.tracks[trackId], nil
}

type erroringNowPlaying struct {
	err error
}

func (f *erroringNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, f.err
}

func TestQueueService_ResumeView_EmbedsCurrentTrack(t *testing.T) {
	repo := newInMemoryQueueRepo()
	reader := &fakeNowPlaying{tracks: map[string]*ports.NowPlayingTrack{
		"y": {Id: "y", Title: "Track Y", Artist: "Artist", AcquisitionStatus: "ready"},
	}}
	svc := NewQueueService(repo, reader)
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	view, err := svc.ResumeView(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.CurrentTrack == nil {
		t.Fatal("expected current track to be embedded")
	}
	if view.CurrentTrack.Id != "y" || view.CurrentTrack.Title != "Track Y" {
		t.Errorf("wrong current track embedded: %+v", view.CurrentTrack)
	}
}

func TestQueueService_ResumeView_UnknownTrackOmitsCurrentTrackWithoutFailing(t *testing.T) {
	repo := newInMemoryQueueRepo()
	readerKnowingNoTracks := &fakeNowPlaying{tracks: map[string]*ports.NowPlayingTrack{}}
	svc := NewQueueService(repo, readerKnowingNoTracks)
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	view, err := svc.ResumeView(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.CurrentTrack != nil {
		t.Errorf("expected no current track for unknown id, got %+v", view.CurrentTrack)
	}
	if view.CurrentTrackUnavailable {
		t.Error("an absent track is not a lookup failure; CurrentTrackUnavailable must stay false")
	}
	if view.State.CurrentIdx != 1 {
		t.Errorf("state should still resume: idx=%d", view.State.CurrentIdx)
	}
}

func TestQueueService_ResumeView_CatalogErrorDegradesButKeepsResume(t *testing.T) {
	repo := newInMemoryQueueRepo()
	catalogTimingOut := &erroringNowPlaying{err: errors.New("catalog db timeout")}
	svc := NewQueueService(repo, catalogTimingOut)
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	view, err := svc.ResumeView(context.Background(), user)
	if err != nil {
		t.Fatalf("catalog enrichment error must not fail the resume: %v", err)
	}
	if view.CurrentTrack != nil {
		t.Errorf("expected no current track when lookup errors, got %+v", view.CurrentTrack)
	}
	if !view.CurrentTrackUnavailable {
		t.Error("a failed lookup must flag CurrentTrackUnavailable so it is not mistaken for nothing playing")
	}
	if view.State.CurrentIdx != 1 || len(view.State.TrackIds) != 2 {
		t.Errorf("queue snapshot must be preserved when enrichment fails: %+v", view.State)
	}
}

// TestQueueService_ResumeView_EnrichmentFailureLogsUserId pins that the
// degraded-enrichment log line carries the owning user, so a failure can be
// attributed to a specific account rather than being an anonymous track_id.
func TestQueueService_ResumeView_EnrichmentFailureLogsUserId(t *testing.T) {
	logs := captureLogs(t)
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo, &erroringNowPlaying{err: errors.New("catalog db timeout")})
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := svc.ResumeView(context.Background(), user); err != nil {
		t.Fatalf("enrichment failure must not fail the resume: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "resume.current_track_enrichment_failed") {
		t.Fatalf("expected an enrichment-failure log line, got %q", out)
	}
	if !strings.Contains(out, user.String()) {
		t.Fatalf("enrichment-failure log line omits user_id; logs=%q", out)
	}
}

func TestQueueService_ResumeView_CanceledLookupDegradesButKeepsResume(t *testing.T) {
	repo := newInMemoryQueueRepo()
	clientDisconnected := &erroringNowPlaying{err: context.Canceled}
	svc := NewQueueService(repo, clientDisconnected)
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	view, err := svc.ResumeView(context.Background(), user)
	if err != nil {
		t.Fatalf("canceled lookup must not become a 500: %v", err)
	}
	if view.CurrentTrack != nil {
		t.Errorf("expected no current track when lookup is canceled, got %+v", view.CurrentTrack)
	}
}

func TestQueueService_ResumeView_UnsavedEmptyQueueOmitsCurrentTrack(t *testing.T) {
	repo := newInMemoryQueueRepo()
	reader := &fakeNowPlaying{tracks: map[string]*ports.NowPlayingTrack{}}
	svc := NewQueueService(repo, reader)
	userWithNoSavedQueue := testUser()

	view, err := svc.ResumeView(context.Background(), userWithNoSavedQueue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.CurrentTrack != nil {
		t.Errorf("expected no current track for empty queue, got %+v", view.CurrentTrack)
	}
}

func TestQueueService_Save_PersistsValidState(t *testing.T) {
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo, &fakeNowPlaying{})
	user := testUser()

	err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"a", "b", "c"},
		CurrentIdx: 2,
		PositionMs: 1000,
		RepeatMode: "all",
		SourceId:   "library",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.GetForUser(context.Background(), user)
	if got == nil {
		t.Fatal("expected state to be persisted")
	}
	if got.CurrentIdx != 2 || got.RepeatMode != domain.RepeatAll {
		t.Errorf("persisted state mismatch: idx=%d repeat=%v", got.CurrentIdx, got.RepeatMode)
	}
}

func TestQueueService_Save_RejectsInvalidRepeatMode(t *testing.T) {
	svc := NewQueueService(newInMemoryQueueRepo(), &fakeNowPlaying{})
	err := svc.Save(context.Background(), testUser(), SaveQueueStateInput{
		TrackIds:   []string{"a"},
		RepeatMode: "bogus",
	})
	if err == nil {
		t.Fatal("expected invalid repeat mode to be rejected")
	}
}

func TestQueueService_Resume_ReturnsEmptyWhenNoneStored(t *testing.T) {
	svc := NewQueueService(newInMemoryQueueRepo(), &fakeNowPlaying{})

	state, err := svc.Resume(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("Resume must never return nil")
	}
	if len(state.TrackIds) != 0 || state.RepeatMode != domain.RepeatOff {
		t.Errorf("expected empty snapshot, got %+v", state)
	}
}

func TestQueueService_Forget_ErasesPersistedState(t *testing.T) {
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo, &fakeNowPlaying{})
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 1,
		RepeatMode: "off",
		SourceId:   "search:mac demarco", // free-text PII in source_id
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if stored, _ := repo.GetForUser(context.Background(), user); stored == nil {
		t.Fatal("precondition: state must be persisted before erasure")
	}

	if err := svc.Forget(context.Background(), user); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	stored, err := repo.GetForUser(context.Background(), user)
	if err != nil {
		t.Fatalf("GetForUser after Forget: %v", err)
	}
	if stored != nil {
		t.Fatalf("queue state (incl. free-text search source_id) survived erasure: %+v", stored)
	}
}

func TestQueueService_Forget_IsIdempotentForUnknownUser(t *testing.T) {
	svc := NewQueueService(newInMemoryQueueRepo(), &fakeNowPlaying{})
	if err := svc.Forget(context.Background(), testUser()); err != nil {
		t.Fatalf("erasing a user with no stored state must not error: %v", err)
	}
}

func TestQueueService_Resume_ReturnsStored(t *testing.T) {
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo, &fakeNowPlaying{})
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "one",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	state, err := svc.Resume(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CurrentIdx != 1 || state.RepeatMode != domain.RepeatOne {
		t.Errorf("resumed state mismatch: %+v", state)
	}
}

type failingQueueRepo struct {
	inMemoryQueueRepo
	err error
}

func (r *failingQueueRepo) GetForUser(_ context.Context, _ shared.UserId) (*domain.QueueState, error) {
	return nil, r.err
}

func corruptStoredRow() error {
	return fmt.Errorf("corrupt stored queue state: invalid repeat mode %q: %w", "sideways", ports.ErrCorruptStoredState)
}

func TestQueueService_Resume_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := NewQueueService(&failingQueueRepo{err: corruptStoredRow()}, &fakeNowPlaying{})
	user := testUser()

	state, err := svc.Resume(context.Background(), user)
	if err != nil {
		t.Fatalf("corrupt stored row must degrade to an empty queue, got error: %v", err)
	}
	if state == nil || len(state.TrackIds) != 0 || state.UserId != user {
		t.Errorf("expected empty queue state for the user, got %+v", state)
	}
}

func TestQueueService_ResumeView_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := NewQueueService(&failingQueueRepo{err: corruptStoredRow()}, &fakeNowPlaying{})

	view, err := svc.ResumeView(context.Background(), testUser())
	if err != nil {
		t.Fatalf("corrupt stored row must not 500 the resume endpoint: %v", err)
	}
	if len(view.State.TrackIds) != 0 || view.CurrentTrack != nil {
		t.Errorf("expected an empty resume view, got %+v", view)
	}
}

func TestQueueService_Resume_InfrastructureErrorStillFails(t *testing.T) {
	dbDown := errors.New("connection refused")
	svc := NewQueueService(&failingQueueRepo{err: dbDown}, &fakeNowPlaying{})

	if _, err := svc.Resume(context.Background(), testUser()); !errors.Is(err, dbDown) {
		t.Fatalf("non-corruption repo errors must still propagate, got %v", err)
	}
}
