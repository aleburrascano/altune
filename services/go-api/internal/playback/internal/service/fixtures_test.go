package service_test

import (
	"altune/go-api/internal/playback/internal/adapters/memory"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
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

func trk(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

type fakeNowPlaying struct {
	Tracks map[string]*ports.NowPlayingTrack
}

func (f *fakeNowPlaying) Lookup(_ context.Context, _ ids.UserId, trackId string) (*ports.NowPlayingTrack, error) {
	track, ok := f.Tracks[trackId]
	if !ok {
		return nil, domain.ErrNowPlayingTrackNotFound
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
	*memory.QueueStateRepo
	Err error
}

func (r *undeletableQueueRepo) DeleteForUser(_ context.Context, _ ids.UserId) error {
	return r.Err
}
