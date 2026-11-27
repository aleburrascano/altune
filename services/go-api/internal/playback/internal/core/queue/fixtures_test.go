package queue_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureJSON(t, slog.LevelDebug)
}

type inMemoryQueueRepo struct {
	States map[uuid.UUID]*playback.QueueState
}

func newInMemoryQueueRepo() *inMemoryQueueRepo {
	return &inMemoryQueueRepo{States: map[uuid.UUID]*playback.QueueState{}}
}

func (r *inMemoryQueueRepo) Upsert(_ context.Context, state *playback.QueueState) error {
	r.States[state.UserId.UUID()] = state
	return nil
}

func (r *inMemoryQueueRepo) UpdatePosition(_ context.Context, position *playback.QueuePosition) error {
	stored := r.States[position.UserId.UUID()]
	if stored == nil || position.CurrentIdx >= len(stored.TrackIds) || stored.TrackIds[position.CurrentIdx] != position.CurrentTrackId {
		return playback.ErrQueuePositionMismatch
	}
	next := *stored
	next.CurrentIdx = position.CurrentIdx
	next.PositionMs = position.PositionMs
	next.UpdatedAt = position.UpdatedAt
	r.States[position.UserId.UUID()] = &next
	return nil
}

func (r *inMemoryQueueRepo) GetForUser(_ context.Context, userId ids.UserId) (*playback.QueueState, error) {
	state, ok := r.States[userId.UUID()]
	if !ok {
		return nil, playback.ErrQueueStateNotFound
	}
	return state, nil
}

func (r *inMemoryQueueRepo) DeleteForUser(_ context.Context, userId ids.UserId) error {
	delete(r.States, userId.UUID())
	return nil
}

func trk(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

type fakeNowPlaying struct {
	Tracks map[string]*playback.NowPlayingTrack
}

func (f *fakeNowPlaying) Lookup(_ context.Context, _ ids.UserId, trackId string) (*playback.NowPlayingTrack, error) {
	track, ok := f.Tracks[trackId]
	if !ok {
		return nil, playback.ErrNowPlayingTrackNotFound
	}
	return track, nil
}

func auditRecord(t *testing.T, logs *logging.Capture, msg string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec["msg"] != msg {
			continue
		}
		if found != nil {
			t.Fatalf("%q emitted more than once; logs=%s", msg, logs.String())
		}
		found = rec
	}
	if found == nil {
		t.Fatalf("no %q audit record; logs=%s", msg, logs.String())
	}
	return found
}

type undeletableQueueRepo struct {
	inMemoryQueueRepo
	Err error
}

func (r *undeletableQueueRepo) DeleteForUser(_ context.Context, _ ids.UserId) error {
	return r.Err
}
