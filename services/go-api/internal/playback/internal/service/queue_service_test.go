package service_test

import (
	"altune/go-api/internal/playback/internal/adapters/memory"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	maxQueueLength      = 10000
	maxQueueStringBytes = 4096
)

type erroringNowPlaying struct {
	err error
}

func (f *erroringNowPlaying) Lookup(_ context.Context, _ ids.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, f.err
}

func TestQueueService_ResumeView_EmbedsCurrentTrack(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	reader := &fakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{
		trk("y"): {Id: trk("y"), Title: "Track Y", Artist: "Artist", AcquisitionStatus: "ready"},
	}}
	svc := service.MustNewQueueService(repo, reader, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), trk("y")},
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
	if view.CurrentTrack.Id != trk("y") || view.CurrentTrack.Title != "Track Y" {
		t.Errorf("wrong current track embedded: %+v", view.CurrentTrack)
	}
}

func TestQueueService_ResumeView_UnknownTrackOmitsCurrentTrackWithoutFailing(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	readerKnowingNoTracks := &fakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{}}
	svc := service.MustNewQueueService(repo, readerKnowingNoTracks, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), trk("y")},
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
	repo := memory.NewQueueStateRepo()
	catalogTimingOut := &erroringNowPlaying{err: errors.New("catalog db timeout")}
	svc := service.MustNewQueueService(repo, catalogTimingOut, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), trk("y")},
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

func TestQueueService_ResumeView_EnrichmentFailureLogsUserId(t *testing.T) {
	logs := captureLogs(t)
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &erroringNowPlaying{err: errors.New("catalog db timeout")}, time.Now)
	user := testUser()
	const nowPlayingId = "5f0c1a52-8d3e-4b6a-9f21-0c7e4d2a9b18"

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), nowPlayingId},
		CurrentIdx: 1,
		RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := svc.ResumeView(logging.ContextWithSubjectUser(context.Background(), user.String()), user); err != nil {
		t.Fatalf("enrichment failure must not fail the resume: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "resume.current_track_enrichment_failed") {
		t.Fatalf("expected an enrichment-failure log line, got %q", out)
	}
	if !strings.Contains(out, `"user_id":"`+user.String()+`"`) {
		t.Fatalf("enrichment-failure log line omits user_id; logs=%q", out)
	}
	if strings.Contains(out, nowPlayingId) || strings.Contains(out, "track_id") {
		t.Fatalf("enrichment-failure log line leaks the raw track id; logs=%q", out)
	}
}

func TestQueueService_ResumeView_CanceledLookupDegradesButKeepsResume(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	clientDisconnected := &erroringNowPlaying{err: context.Canceled}
	svc := service.MustNewQueueService(repo, clientDisconnected, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), trk("y")},
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
	repo := memory.NewQueueStateRepo()
	reader := &fakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{}}
	svc := service.MustNewQueueService(repo, reader, time.Now)
	userWithNoSavedQueue := testUser()

	view, err := svc.ResumeView(context.Background(), userWithNoSavedQueue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.CurrentTrack != nil {
		t.Errorf("expected no current track for empty queue, got %+v", view.CurrentTrack)
	}
}

func TestQueueService_ResumeView_OutOfRangeIdxOmitsCurrentTrack(t *testing.T) {
	for _, tt := range []struct {
		name     string
		trackIds []string
		idx      int
	}{
		{name: "empty queue non-zero idx", trackIds: []string{}, idx: 3},
		{name: "idx past end", trackIds: []string{trk("x")}, idx: 1},
		{name: "negative idx", trackIds: []string{trk("x")}, idx: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := memory.NewQueueStateRepo()
			failIfLookedUp := &erroringNowPlaying{err: errors.New("lookup must not be called")}
			svc := service.MustNewQueueService(repo, failIfLookedUp, time.Now)
			user := testUser()
			repo.Seed(&domain.QueueState{UserId: user, TrackIds: tt.trackIds, CurrentIdx: tt.idx})

			view, err := svc.ResumeView(context.Background(), user)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.CurrentTrack != nil || view.CurrentTrackUnavailable {
				t.Errorf("expected no current track and no lookup, got %+v", view)
			}
		})
	}
}

func TestQueueService_Save_PersistsValidState(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	user := testUser()

	err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:       []string{trk("a"), trk("b"), trk("c")},
		CurrentIdx:     2,
		PositionMs:     1000,
		RepeatMode:     "all",
		LegacySourceId: "library",
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
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)
	err := svc.Save(context.Background(), testUser(), service.SaveQueueStateInput{
		TrackIds:   []string{trk("a")},
		RepeatMode: "bogus",
	})
	if err == nil {
		t.Fatal("expected invalid repeat mode to be rejected")
	}
}

func TestQueueService_Save_FormatsTheStructuredSource(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	user := testUser()

	err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		RepeatMode:     "off",
		Source:         domain.QueueSource{Kind: "playlist", PlaylistId: "a:b", Name: "road trip"},
		LegacySourceId: "library",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, _ := repo.GetForUser(context.Background(), user)
	if got == nil || got.SourceId != "playlist:a%3Ab:road+trip" {
		t.Fatalf("stored source_id = %+v, want the structured source formatted over the legacy fallback", got)
	}
}

func TestQueueService_Save_RejectsAMalformedSourceBeforeTheRepeatMode(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)

	err := svc.Save(context.Background(), testUser(), service.SaveQueueStateInput{RepeatMode: "sideways", LegacySourceId: "mixtape:7"})

	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) || invalid.Kind != domain.KindUnrecognizedSourceId {
		t.Fatalf("Save = %v, want the source error to win over the repeat-mode error", err)
	}
	if repo.Len() != 0 {
		t.Fatalf("a rejected source was stored: %d states", repo.Len())
	}
}

func TestQueueService_StampsEveryStateWithTheInjectedClock(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	clock := time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, func() time.Time { return clock })
	user := testUser()
	ctx := context.Background()

	empty, err := svc.Resume(ctx, user)
	if err != nil || !empty.UpdatedAt.Equal(clock) {
		t.Fatalf("Resume of an unknown user = %v/%v, want an empty state stamped %v", empty, err, clock)
	}
	clock = clock.Add(time.Minute)
	if err := svc.Save(ctx, user, service.SaveQueueStateInput{TrackIds: []string{trk("a"), trk("b")}, NaturalOrder: []string{trk("a"), trk("b")}, RepeatMode: "off"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, _ := repo.GetForUser(ctx, user)
	if saved == nil || !saved.UpdatedAt.Equal(clock) {
		t.Fatalf("saved state = %+v, want UpdatedAt %v", saved, clock)
	}
	clock = clock.Add(time.Minute)
	if err := svc.SavePosition(ctx, user, service.SaveQueuePositionInput{CurrentIdx: 1, CurrentTrackId: trk("b"), PositionMs: 5}); err != nil {
		t.Fatalf("SavePosition: %v", err)
	}
	moved, _ := repo.GetForUser(ctx, user)
	if moved == nil || !moved.UpdatedAt.Equal(clock) {
		t.Fatalf("state after a position save = %+v, want UpdatedAt %v", moved, clock)
	}
}

func TestNewQueueService_RefusesANilClock(t *testing.T) {
	svc, err := service.NewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, nil)

	if !errors.Is(err, service.ErrQueueClockRequired) || svc != nil {
		t.Fatalf("NewQueueService(nil clock) = (%v, %v), want ErrQueueClockRequired and no service", svc, err)
	}
	if !strings.Contains(err.Error(), "NewQueueService") {
		t.Fatalf("error %q does not name the constructor", err)
	}
}

func TestMustNewQueueService_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewQueueService accepted a nil clock")
		}
	}()

	service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, nil)
}

func TestQueueService_SavePosition_MovesPositionWithinStoredQueue(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	user := testUser()
	ctx := context.Background()
	if err := svc.Save(ctx, user, service.SaveQueueStateInput{
		TrackIds: []string{trk("a"), trk("b"), trk("c")}, NaturalOrder: []string{trk("c"), trk("b"), trk("a")}, CurrentIdx: 0, PositionMs: 1000, RepeatMode: "all",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := svc.SavePosition(ctx, user, service.SaveQueuePositionInput{CurrentIdx: 1, CurrentTrackId: trk("b"), PositionMs: 42000}); err != nil {
		t.Fatalf("SavePosition: %v", err)
	}

	got, _ := repo.GetForUser(ctx, user)
	if got == nil {
		t.Fatal("stored state vanished after a position save")
	}
	if got.CurrentIdx != 1 || got.PositionMs != 42000 {
		t.Errorf("position not saved: idx=%d position=%d", got.CurrentIdx, got.PositionMs)
	}
	if strings.Join(got.TrackIds, ",") != strings.Join([]string{trk("a"), trk("b"), trk("c")}, ",") || strings.Join(got.NaturalOrder, ",") != strings.Join([]string{trk("c"), trk("b"), trk("a")}, ",") || got.RepeatMode != domain.RepeatAll {
		t.Errorf("a position-only save changed the rest of the queue: %+v", got)
	}
}

func TestQueueService_SavePosition_RejectsInvalidInputWithoutWriting(t *testing.T) {
	tests := []struct {
		name  string
		input service.SaveQueuePositionInput
	}{
		{name: "missing track id", input: service.SaveQueuePositionInput{CurrentIdx: 0, PositionMs: 1}},
		{name: "negative position", input: service.SaveQueuePositionInput{CurrentTrackId: trk("a"), PositionMs: -1}},
		{name: "negative index", input: service.SaveQueuePositionInput{CurrentIdx: -1, CurrentTrackId: trk("a")}},
		{name: "index past max queue", input: service.SaveQueuePositionInput{CurrentIdx: maxQueueLength, CurrentTrackId: trk("a")}},
		{name: "NUL in track id", input: service.SaveQueuePositionInput{CurrentTrackId: "a\x00"}},
		{name: "oversized track id", input: service.SaveQueuePositionInput{CurrentTrackId: strings.Repeat("a", maxQueueStringBytes+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.MustNewQueueService(&panickingRepo{}, &fakeNowPlaying{}, time.Now)
			err := svc.SavePosition(context.Background(), testUser(), tt.input)
			var invalid *domain.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("SavePosition(%+v) = %v, want a validation error", tt.input, err)
			}
		})
	}
}

func TestQueueService_SavePosition_UnmatchedQueueIsConflict(t *testing.T) {
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)

	err := svc.SavePosition(context.Background(), testUser(), service.SaveQueuePositionInput{CurrentTrackId: trk("a"), PositionMs: 1})

	if !errors.Is(err, domain.ErrQueuePositionMismatch) {
		t.Fatalf("SavePosition with nothing stored = %v, want ErrQueuePositionMismatch", err)
	}
}

type panickingRepo struct{ *memory.QueueStateRepo }

func (*panickingRepo) UpdatePosition(context.Context, *domain.QueuePosition) error {
	panic("an invalid position reached the repository")
}

func TestQueueService_Resume_ReturnsEmptyWhenNoneStored(t *testing.T) {
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)

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
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:       []string{trk("a"), trk("b")},
		CurrentIdx:     1,
		RepeatMode:     "off",
		LegacySourceId: "search:mac demarco",
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
	if !errors.Is(err, domain.ErrQueueStateNotFound) {
		t.Fatalf("GetForUser after Forget: err = %v, want ErrQueueStateNotFound", err)
	}
	if stored != nil {
		t.Fatalf("queue state (incl. free-text search source_id) survived erasure: %+v", stored)
	}
}

func TestQueueService_Forget_IsIdempotentForUnknownUser(t *testing.T) {
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)
	if err := svc.Forget(context.Background(), testUser()); err != nil {
		t.Fatalf("erasing a user with no stored state must not error: %v", err)
	}
}

func TestQueueService_Forget_LeavesAnAuditRecord(t *testing.T) {
	logs := captureLogs(t)
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)
	user := testUser()
	const privateSourceId = "search:mac demarco"
	const privateTrackId = "3b9c77e4-5d1a-4f0e-9c2b-7a6e1f4d8c30"
	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:       []string{trk("a"), privateTrackId},
		CurrentIdx:     1,
		RepeatMode:     "off",
		LegacySourceId: privateSourceId,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	erasureRequested := time.Now().UTC()

	if err := svc.Forget(context.Background(), user); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	rec := auditRecord(t, logs, "playback.queue_state_forgotten")
	if rec["action"] != "queue_state.forget" {
		t.Errorf("action = %v, want queue_state.forget", rec["action"])
	}
	if rec["user_id"] != user.String() {
		t.Errorf("actor user_id = %v, want %q", rec["user_id"], user.String())
	}
	if rec["object"] != "playback_queue_state" {
		t.Errorf("object = %v, want playback_queue_state", rec["object"])
	}
	if rec["trigger"] != "self" {
		t.Errorf("trigger = %v, want self", rec["trigger"])
	}
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(rec["time"]))
	if err != nil {
		t.Fatalf("time = %v is not a timestamp: %v", rec["time"], err)
	}
	if at.Before(erasureRequested.Add(-time.Second)) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("time = %v, want the instant of the erasure (~%v)", at, erasureRequested)
	}
	if out := logs.String(); strings.Contains(out, privateSourceId) || strings.Contains(out, privateTrackId) {
		t.Errorf("the audit trail leaks the PII it says was erased; logs=%s", out)
	}
}

func TestQueueService_Forget_FailedErasureIsNotAudited(t *testing.T) {
	logs := captureLogs(t)
	dbDown := errors.New("connection refused")
	svc := service.MustNewQueueService(&undeletableQueueRepo{Err: dbDown}, &fakeNowPlaying{}, time.Now)

	if err := svc.Forget(context.Background(), testUser()); !errors.Is(err, dbDown) {
		t.Fatalf("a failed delete must propagate, got %v", err)
	}

	if out := logs.String(); strings.Contains(out, "playback.queue_state_forgotten") {
		t.Errorf("an erasure that never happened was audited as done; logs=%s", out)
	}
}

func TestQueueService_Resume_ReturnsStored(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	user := testUser()

	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds:   []string{trk("x"), trk("y")},
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
	*memory.QueueStateRepo
	err error
}

func (r *failingQueueRepo) GetForUser(_ context.Context, _ ids.UserId) (*domain.QueueState, error) {
	return nil, r.err
}

func corruptStoredRow() error {
	return fmt.Errorf("corrupt stored queue state: invalid repeat mode %q: %w", "sideways", domain.ErrCorruptStoredState)
}

func TestQueueService_Resume_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := service.MustNewQueueService(&failingQueueRepo{err: corruptStoredRow()}, &fakeNowPlaying{}, time.Now)
	user := testUser()

	state, err := svc.Resume(context.Background(), user)
	if err != nil {
		t.Fatalf("corrupt stored row must degrade to an empty queue, got error: %v", err)
	}
	if state == nil || len(state.TrackIds) != 0 || state.UserId != user {
		t.Errorf("expected empty queue state for the user, got %+v", state)
	}
}

func TestQueueService_Resume_CorruptStoredRowIsStampedWithTheInjectedClock(t *testing.T) {
	stamp := time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)
	svc := service.MustNewQueueService(&failingQueueRepo{err: corruptStoredRow()}, &fakeNowPlaying{}, func() time.Time { return stamp })

	state, err := svc.Resume(context.Background(), testUser())
	if err != nil {
		t.Fatalf("corrupt stored row must degrade to an empty queue, got error: %v", err)
	}
	if !state.UpdatedAt.Equal(stamp) {
		t.Errorf("empty state UpdatedAt = %v, want the injected clock's %v", state.UpdatedAt, stamp)
	}
}

func TestQueueService_ResumeView_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := service.MustNewQueueService(&failingQueueRepo{err: corruptStoredRow()}, &fakeNowPlaying{}, time.Now)

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
	svc := service.MustNewQueueService(&failingQueueRepo{err: dbDown}, &fakeNowPlaying{}, time.Now)

	if _, err := svc.Resume(context.Background(), testUser()); !errors.Is(err, dbDown) {
		t.Fatalf("non-corruption repo errors must still propagate, got %v", err)
	}
}

type countingNowPlaying struct {
	calls int
}

func (c *countingNowPlaying) Lookup(_ context.Context, _ ids.UserId, trackId string) (*ports.NowPlayingTrack, error) {
	c.calls++
	return &ports.NowPlayingTrack{Id: trackId, Title: "Track " + trackId}, nil
}

func TestQueueService_ResumeView_LooksUpTheNowPlayingTrackWhilePlaying(t *testing.T) {
	reader := &countingNowPlaying{}
	svc := service.MustNewQueueService(memory.NewQueueStateRepo(), reader, time.Now)
	user := testUser()
	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
		TrackIds: []string{trk("x"), trk("y")}, CurrentIdx: 1, RepeatMode: "off",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	view, err := svc.ResumeView(context.Background(), user)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if reader.calls != 1 {
		t.Errorf("nowPlaying.Lookup calls = %d, want 1 while a track is playing", reader.calls)
	}
	if view.CurrentTrack == nil || view.CurrentTrack.Id != trk("y") {
		t.Errorf("current track = %+v, want the playing track %s embedded", view.CurrentTrack, trk("y"))
	}
	if view.CurrentTrackUnavailable {
		t.Error("a successful lookup must not mark the current track unavailable")
	}
}
