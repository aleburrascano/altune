package playbacktest

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func CaptureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type InMemoryQueueRepo struct {
	States map[uuid.UUID]*domain.QueueState
}

func NewInMemoryQueueRepo() *InMemoryQueueRepo {
	return &InMemoryQueueRepo{States: map[uuid.UUID]*domain.QueueState{}}
}

func (r *InMemoryQueueRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	r.States[state.UserId.UUID()] = state
	return nil
}

func (r *InMemoryQueueRepo) UpdatePosition(_ context.Context, position *domain.QueuePosition) error {
	stored := r.States[position.UserId.UUID()]
	if stored == nil || position.CurrentIdx >= len(stored.TrackIds) || stored.TrackIds[position.CurrentIdx] != position.CurrentTrackId {
		return domain.ErrQueuePositionMismatch
	}
	next := *stored
	next.CurrentIdx = position.CurrentIdx
	next.PositionMs = position.PositionMs
	next.UpdatedAt = position.UpdatedAt
	r.States[position.UserId.UUID()] = &next
	return nil
}

func (r *InMemoryQueueRepo) GetForUser(_ context.Context, userId shared.UserId) (*domain.QueueState, error) {
	return r.States[userId.UUID()], nil
}

func (r *InMemoryQueueRepo) DeleteForUser(_ context.Context, userId shared.UserId) error {
	delete(r.States, userId.UUID())
	return nil
}

func Trk(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func TestUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

type FakeNowPlaying struct {
	Tracks map[string]*ports.NowPlayingTrack
}

func (f *FakeNowPlaying) Lookup(_ context.Context, _ shared.UserId, trackId string) (*ports.NowPlayingTrack, error) {
	return f.Tracks[trackId], nil
}

func AuditRecord(t *testing.T, logs *bytes.Buffer, msg string) map[string]any {
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

type UndeletableQueueRepo struct {
	InMemoryQueueRepo
	Err error
}

func (r *UndeletableQueueRepo) DeleteForUser(_ context.Context, _ shared.UserId) error {
	return r.Err
}
