package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

// testOtherUserId is a second owner, for owner-scoping tests.
func testOtherUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
}

func seedTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func seedReadyTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := seedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func seedPlaylist(t *testing.T, repo *catalogtest.PlaylistRepo, userId shared.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name, time.Now())
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}

func ptrStatus(s domain.AcquisitionStatus) *domain.AcquisitionStatus {
	return &s
}

// auditRecord is one captured log line flattened to string attrs.
type auditRecord struct {
	msg   string
	time  time.Time
	attrs map[string]string
}

type auditCapture struct {
	mu      sync.Mutex
	records []auditRecord
}

func (h *auditCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *auditCapture) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *auditCapture) WithGroup(string) slog.Handler { return h }

func (h *auditCapture) Handle(_ context.Context, r slog.Record) error {
	rec := auditRecord{msg: r.Message, time: r.Time, attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *auditCapture) find(t *testing.T, msg string) auditRecord {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.msg == msg {
			return r
		}
	}
	t.Fatalf("no %q log line; destructive action left no attributable trail", msg)
	return auditRecord{}
}

func captureAuditLogs(t *testing.T) *auditCapture {
	t.Helper()
	h := &auditCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func assertAttrs(t *testing.T, rec auditRecord, want map[string]string) {
	t.Helper()
	if rec.time.IsZero() {
		t.Errorf("%q: record has no time", rec.msg)
	}
	for k, v := range want {
		if got := rec.attrs[k]; got != v {
			t.Errorf("%q: attr %s = %q, want %q", rec.msg, k, got, v)
		}
	}
}

// stuckScheduler stands in for a Schedule call that never returns on its own:
// it records the deadline it was handed and blocks until its context ends.
type stuckScheduler struct {
	deadline    time.Time
	hasDeadline bool
}

func (s *stuckScheduler) Schedule(ctx context.Context, _ shared.UserId, _ domain.TrackId, _ string) error {
	s.deadline, s.hasDeadline = ctx.Deadline()
	<-ctx.Done()
	return ctx.Err()
}

func assertScheduleDeadline(t *testing.T, sched *stuckScheduler, start time.Time) {
	t.Helper()
	if !sched.hasDeadline {
		t.Fatal("Schedule received a context with no deadline")
	}
	// The call happened between start and now, so its deadline must land in
	// (start, now+scheduleTimeout].
	latest := time.Now().Add(scheduleTimeout)
	if !sched.deadline.After(start) || sched.deadline.After(latest) {
		t.Errorf("Schedule deadline = %v, want within (%v, %v]", sched.deadline, start, latest)
	}
}
