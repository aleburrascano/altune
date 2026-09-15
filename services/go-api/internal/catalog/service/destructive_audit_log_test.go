package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

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
func (h *auditCapture) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *auditCapture) WithGroup(string) slog.Handler            { return h }

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

// TestDeleteTrackService_LogsActorAndObject pins #1052: a successful track
// delete records who (user_id) deleted what (track_id) and when.
func TestDeleteTrackService_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	track := seedTrack(t, repo, userId, "Track", "Artist", "Album")

	if err := NewDeleteTrackService(repo, catalogtest.NewAudioStore()).Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAttrs(t, logs.find(t, "track deleted from library"), map[string]string{
		"user_id":  userId.String(),
		"track_id": track.ID.String(),
	})
}

// TestDeleteTrackService_OrphanLogCarriesActor pins #1052: the orphan-failure
// line names the user whose delete orphaned the audio, and the partial delete
// still leaves the deletion trail.
func TestDeleteTrackService_OrphanLogCarriesActor(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	track := seedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/gone.opus")
	store := catalogtest.NewAudioStore()
	store.ErrOnDelete = errors.New("s3 down")

	err := NewDeleteTrackService(repo, store).Execute(context.Background(), userId, track.ID)
	if !errors.Is(err, ErrAudioOrphaned) {
		t.Fatalf("error = %v, want ErrAudioOrphaned", err)
	}

	assertAttrs(t, logs.find(t, "orphaned audio file after track delete"), map[string]string{
		"user_id":  userId.String(),
		"track_id": track.ID.String(),
	})
	logs.find(t, "track deleted from library")
}

// TestPlaylistMembershipService_RemoveTrack_LogsActorAndObject pins #1052.
func TestPlaylistMembershipService_RemoveTrack_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "Mix")
	trackId := domain.NewTrackId()
	if err := pl.AddTrack(trackId, time.Now()); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo())
	if err := svc.RemoveTrack(context.Background(), userId, pl.ID, trackId); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAttrs(t, logs.find(t, "track removed from playlist"), map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"track_id":    trackId.String(),
	})
}

// TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject pins #1052:
// the batch line names every track actually removed, not the ones requested.
func TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "Mix")
	a, b := domain.NewTrackId(), domain.NewTrackId()
	for _, id := range []domain.TrackId{a, b} {
		if err := pl.AddTrack(id, time.Now()); err != nil {
			t.Fatalf("seed track: %v", err)
		}
	}
	absent := domain.NewTrackId()

	svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo())
	n, err := svc.RemoveTracks(context.Background(), userId, pl.ID, []domain.TrackId{a, absent, b})
	if err != nil || n != 2 {
		t.Fatalf("RemoveTracks = %d, %v; want 2, nil", n, err)
	}

	rec := logs.find(t, "tracks removed from playlist")
	assertAttrs(t, rec, map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"removed":     "2",
		"requested":   "3",
	})
	ids := rec.attrs["track_ids"]
	if !strings.Contains(ids, a.String()) || !strings.Contains(ids, b.String()) || strings.Contains(ids, absent.String()) {
		t.Errorf("track_ids = %q, want exactly the removed ids %s and %s", ids, a, b)
	}
}
