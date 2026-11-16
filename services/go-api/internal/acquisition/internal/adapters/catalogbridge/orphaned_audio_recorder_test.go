package catalogbridge

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type stubQueue struct {
	got catalog.OrphanedAudio
	err error
}

func (q *stubQueue) RecordOrphanedAudio(_ context.Context, o catalog.OrphanedAudio) error {
	q.got = o
	return q.err
}

func TestOrphanedAudioRecorderPassesFieldsThrough(t *testing.T) {
	q := &stubQueue{}
	orphan := ports.OrphanedAudio{AudioRef: "u/a.mp3", UserId: shared.NewUserId(uuid.New()), TrackId: shared.NewTrackId()}

	if err := NewOrphanedAudioRecorder(q).RecordOrphanedAudio(context.Background(), orphan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := catalog.OrphanedAudio{AudioRef: orphan.AudioRef, UserId: orphan.UserId, TrackId: orphan.TrackId}
	if q.got != want {
		t.Errorf("got %+v, want %+v", q.got, want)
	}
}

func TestOrphanedAudioRecorderReturnsQueueError(t *testing.T) {
	boom := errors.New("queue down")
	q := &stubQueue{err: boom}

	err := NewOrphanedAudioRecorder(q).RecordOrphanedAudio(context.Background(), ports.OrphanedAudio{})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}
