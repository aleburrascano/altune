package queue

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/playbacktest"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type erroringNowPlaying struct {
	err error
}

func (f *erroringNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, f.err
}

func TestQueueService_ResumeView_EmbedsCurrentTrack(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	reader := &playbacktest.FakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{
		playbacktest.Trk("y"): {Id: playbacktest.Trk("y"), Title: "Track Y", Artist: "Artist", AcquisitionStatus: "ready"},
	}}
	svc := New(repo, reader)
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), playbacktest.Trk("y")},
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
	if view.CurrentTrack.Id != playbacktest.Trk("y") || view.CurrentTrack.Title != "Track Y" {
		t.Errorf("wrong current track embedded: %+v", view.CurrentTrack)
	}
}

func TestQueueService_ResumeView_UnknownTrackOmitsCurrentTrackWithoutFailing(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	readerKnowingNoTracks := &playbacktest.FakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{}}
	svc := New(repo, readerKnowingNoTracks)
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), playbacktest.Trk("y")},
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
	repo := playbacktest.NewInMemoryQueueRepo()
	catalogTimingOut := &erroringNowPlaying{err: errors.New("catalog db timeout")}
	svc := New(repo, catalogTimingOut)
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), playbacktest.Trk("y")},
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
	logs := playbacktest.CaptureLogs(t)
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &erroringNowPlaying{err: errors.New("catalog db timeout")})
	user := playbacktest.TestUser()
	const nowPlayingId = "5f0c1a52-8d3e-4b6a-9f21-0c7e4d2a9b18"

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), nowPlayingId},
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
	if !strings.Contains(out, `"user_id":"`+user.String()+`"`) {
		t.Fatalf("enrichment-failure log line omits user_id; logs=%q", out)
	}
	if strings.Contains(out, nowPlayingId) || strings.Contains(out, "track_id") {
		t.Fatalf("enrichment-failure log line leaks the raw track id; logs=%q", out)
	}
}

func TestQueueService_ResumeView_CanceledLookupDegradesButKeepsResume(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	clientDisconnected := &erroringNowPlaying{err: context.Canceled}
	svc := New(repo, clientDisconnected)
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), playbacktest.Trk("y")},
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
	repo := playbacktest.NewInMemoryQueueRepo()
	reader := &playbacktest.FakeNowPlaying{Tracks: map[string]*ports.NowPlayingTrack{}}
	svc := New(repo, reader)
	userWithNoSavedQueue := playbacktest.TestUser()

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
		{name: "idx past end", trackIds: []string{playbacktest.Trk("x")}, idx: 1},
		{name: "negative idx", trackIds: []string{playbacktest.Trk("x")}, idx: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := playbacktest.NewInMemoryQueueRepo()
			failIfLookedUp := &erroringNowPlaying{err: errors.New("lookup must not be called")}
			svc := New(repo, failIfLookedUp)
			user := playbacktest.TestUser()
			repo.States[user.UUID()] = &domain.QueueState{UserId: user, TrackIds: tt.trackIds, CurrentIdx: tt.idx}

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
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()

	err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:       []string{playbacktest.Trk("a"), playbacktest.Trk("b"), playbacktest.Trk("c")},
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
	svc := New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{})
	err := svc.Save(context.Background(), playbacktest.TestUser(), SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("a")},
		RepeatMode: "bogus",
	})
	if err == nil {
		t.Fatal("expected invalid repeat mode to be rejected")
	}
}

func TestQueueService_Save_FormatsTheStructuredSource(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()

	err := svc.Save(context.Background(), user, SaveQueueStateInput{
		RepeatMode:     "off",
		Source:         domain.QueueSource{Kind: domain.SourceKindPlaylist, PlaylistId: "a:b", Name: "road trip"},
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
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})

	err := svc.Save(context.Background(), playbacktest.TestUser(), SaveQueueStateInput{RepeatMode: "sideways", LegacySourceId: "mixtape:7"})

	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "playback.unrecognized_source_id" {
		t.Fatalf("Save = %v, want the source error to win over the repeat-mode error", err)
	}
	if len(repo.States) != 0 {
		t.Fatalf("a rejected source was stored: %+v", repo.States)
	}
}

func TestQueueService_SavePosition_MovesPositionWithinStoredQueue(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()
	ctx := context.Background()
	if err := svc.Save(ctx, user, SaveQueueStateInput{
		TrackIds: []string{playbacktest.Trk("a"), playbacktest.Trk("b"), playbacktest.Trk("c")}, NaturalOrder: []string{playbacktest.Trk("c"), playbacktest.Trk("b"), playbacktest.Trk("a")}, CurrentIdx: 0, PositionMs: 1000, RepeatMode: "all",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := svc.SavePosition(ctx, user, SaveQueuePositionInput{CurrentIdx: 1, CurrentTrackId: playbacktest.Trk("b"), PositionMs: 42000}); err != nil {
		t.Fatalf("SavePosition: %v", err)
	}

	got, _ := repo.GetForUser(ctx, user)
	if got == nil {
		t.Fatal("stored state vanished after a position save")
	}
	if got.CurrentIdx != 1 || got.PositionMs != 42000 {
		t.Errorf("position not saved: idx=%d position=%d", got.CurrentIdx, got.PositionMs)
	}
	if strings.Join(got.TrackIds, ",") != strings.Join([]string{playbacktest.Trk("a"), playbacktest.Trk("b"), playbacktest.Trk("c")}, ",") || strings.Join(got.NaturalOrder, ",") != strings.Join([]string{playbacktest.Trk("c"), playbacktest.Trk("b"), playbacktest.Trk("a")}, ",") || got.RepeatMode != domain.RepeatAll {
		t.Errorf("a position-only save changed the rest of the queue: %+v", got)
	}
}

func TestQueueService_SavePosition_RejectsInvalidInputWithoutWriting(t *testing.T) {
	tests := []struct {
		name  string
		input SaveQueuePositionInput
	}{
		{name: "missing track id", input: SaveQueuePositionInput{CurrentIdx: 0, PositionMs: 1}},
		{name: "negative position", input: SaveQueuePositionInput{CurrentTrackId: playbacktest.Trk("a"), PositionMs: -1}},
		{name: "negative index", input: SaveQueuePositionInput{CurrentIdx: -1, CurrentTrackId: playbacktest.Trk("a")}},
		{name: "index past max queue", input: SaveQueuePositionInput{CurrentIdx: domain.MaxQueueLength, CurrentTrackId: playbacktest.Trk("a")}},
		{name: "NUL in track id", input: SaveQueuePositionInput{CurrentTrackId: "a\x00"}},
		{name: "oversized track id", input: SaveQueuePositionInput{CurrentTrackId: strings.Repeat("a", domain.MaxQueueStringBytes+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(&panickingRepo{}, &playbacktest.FakeNowPlaying{})
			err := svc.SavePosition(context.Background(), playbacktest.TestUser(), tt.input)
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("SavePosition(%+v) = %v, want a validation error", tt.input, err)
			}
		})
	}
}

func TestQueueService_SavePosition_UnmatchedQueueIsConflict(t *testing.T) {
	svc := New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{})

	err := svc.SavePosition(context.Background(), playbacktest.TestUser(), SaveQueuePositionInput{CurrentTrackId: playbacktest.Trk("a"), PositionMs: 1})

	if !errors.Is(err, domain.ErrQueuePositionMismatch) {
		t.Fatalf("SavePosition with nothing stored = %v, want ErrQueuePositionMismatch", err)
	}
}

type panickingRepo struct{ playbacktest.InMemoryQueueRepo }

func (*panickingRepo) UpdatePosition(context.Context, *domain.QueuePosition) error {
	panic("an invalid position reached the repository")
}

func TestQueueService_Resume_ReturnsEmptyWhenNoneStored(t *testing.T) {
	svc := New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{})

	state, err := svc.Resume(context.Background(), playbacktest.TestUser())
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
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:       []string{playbacktest.Trk("a"), playbacktest.Trk("b")},
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
	if err != nil {
		t.Fatalf("GetForUser after Forget: %v", err)
	}
	if stored != nil {
		t.Fatalf("queue state (incl. free-text search source_id) survived erasure: %+v", stored)
	}
}

func TestQueueService_Forget_IsIdempotentForUnknownUser(t *testing.T) {
	svc := New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{})
	if err := svc.Forget(context.Background(), playbacktest.TestUser()); err != nil {
		t.Fatalf("erasing a user with no stored state must not error: %v", err)
	}
}

func TestQueueService_Forget_LeavesAnAuditRecord(t *testing.T) {
	logs := playbacktest.CaptureLogs(t)
	svc := New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()
	const privateSourceId = "search:mac demarco"
	const privateTrackId = "3b9c77e4-5d1a-4f0e-9c2b-7a6e1f4d8c30"
	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:       []string{playbacktest.Trk("a"), privateTrackId},
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

	rec := playbacktest.AuditRecord(t, logs, "playback.queue_state_forgotten")
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
	logs := playbacktest.CaptureLogs(t)
	dbDown := errors.New("connection refused")
	svc := New(&playbacktest.UndeletableQueueRepo{Err: dbDown}, &playbacktest.FakeNowPlaying{})

	if err := svc.Forget(context.Background(), playbacktest.TestUser()); !errors.Is(err, dbDown) {
		t.Fatalf("a failed delete must propagate, got %v", err)
	}

	if out := logs.String(); strings.Contains(out, "playback.queue_state_forgotten") {
		t.Errorf("an erasure that never happened was audited as done; logs=%s", out)
	}
}

func TestQueueService_Resume_ReturnsStored(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	svc := New(repo, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{playbacktest.Trk("x"), playbacktest.Trk("y")},
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
	playbacktest.InMemoryQueueRepo
	err error
}

func (r *failingQueueRepo) GetForUser(_ context.Context, _ shared.UserId) (*domain.QueueState, error) {
	return nil, r.err
}

func corruptStoredRow() error {
	return fmt.Errorf("corrupt stored queue state: invalid repeat mode %q: %w", "sideways", ports.ErrCorruptStoredState)
}

func TestQueueService_Resume_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := New(&failingQueueRepo{err: corruptStoredRow()}, &playbacktest.FakeNowPlaying{})
	user := playbacktest.TestUser()

	state, err := svc.Resume(context.Background(), user)
	if err != nil {
		t.Fatalf("corrupt stored row must degrade to an empty queue, got error: %v", err)
	}
	if state == nil || len(state.TrackIds) != 0 || state.UserId != user {
		t.Errorf("expected empty queue state for the user, got %+v", state)
	}
}

func TestQueueService_ResumeView_CorruptStoredRowFallsBackToEmpty(t *testing.T) {
	svc := New(&failingQueueRepo{err: corruptStoredRow()}, &playbacktest.FakeNowPlaying{})

	view, err := svc.ResumeView(context.Background(), playbacktest.TestUser())
	if err != nil {
		t.Fatalf("corrupt stored row must not 500 the resume endpoint: %v", err)
	}
	if len(view.State.TrackIds) != 0 || view.CurrentTrack != nil {
		t.Errorf("expected an empty resume view, got %+v", view)
	}
}

func TestQueueService_Resume_InfrastructureErrorStillFails(t *testing.T) {
	dbDown := errors.New("connection refused")
	svc := New(&failingQueueRepo{err: dbDown}, &playbacktest.FakeNowPlaying{})

	if _, err := svc.Resume(context.Background(), playbacktest.TestUser()); !errors.Is(err, dbDown) {
		t.Fatalf("non-corruption repo errors must still propagate, got %v", err)
	}
}

type countingNowPlaying struct {
	calls int
}

func (c *countingNowPlaying) Lookup(_ context.Context, _ shared.UserId, trackId string) (*ports.NowPlayingTrack, error) {
	c.calls++
	return &ports.NowPlayingTrack{Id: trackId, Title: "Track " + trackId}, nil
}

func TestQueueService_ResumeView_NowPlayingEnrichmentKillSwitch(t *testing.T) {
	tests := []struct {
		name      string
		opts      []Option
		wantCalls int
		wantTrack bool
	}{
		{name: "default enabled", opts: nil, wantCalls: 1, wantTrack: true},
		{name: "explicitly enabled", opts: []Option{WithNowPlayingEnrichment(true)}, wantCalls: 1, wantTrack: true},
		{name: "disabled", opts: []Option{WithNowPlayingEnrichment(false)}, wantCalls: 0, wantTrack: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &countingNowPlaying{}
			svc := New(playbacktest.NewInMemoryQueueRepo(), reader, tt.opts...)
			user := playbacktest.TestUser()
			if err := svc.Save(context.Background(), user, SaveQueueStateInput{
				TrackIds: []string{playbacktest.Trk("x"), playbacktest.Trk("y")}, CurrentIdx: 1, RepeatMode: "off",
			}); err != nil {
				t.Fatalf("save: %v", err)
			}

			view, err := svc.ResumeView(context.Background(), user)
			if err != nil {
				t.Fatalf("resume must succeed regardless of the switch: %v", err)
			}
			if reader.calls != tt.wantCalls {
				t.Errorf("nowPlaying.Lookup calls = %d, want %d", reader.calls, tt.wantCalls)
			}
			if got := view.CurrentTrack != nil; got != tt.wantTrack {
				t.Errorf("current track embedded = %v, want %v (%+v)", got, tt.wantTrack, view.CurrentTrack)
			}
			if view.CurrentTrackUnavailable {
				t.Error("a disabled lookup is an operator choice, not a dependency fault; CurrentTrackUnavailable must stay false")
			}
			if view.State == nil || len(view.State.TrackIds) != 2 || view.State.CurrentIdx != 1 {
				t.Errorf("resume must still return the stored queue, got %+v", view.State)
			}
		})
	}
}
