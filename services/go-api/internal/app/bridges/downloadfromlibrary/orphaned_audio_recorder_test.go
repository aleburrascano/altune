package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type stubQueue struct {
	got library.OrphanedAudio
	err error
}

func (q *stubQueue) RecordOrphanedAudio(_ context.Context, o library.OrphanedAudio) error {
	q.got = o
	return q.err
}

func TestOrphanedAudioRecorderPassesFieldsThrough(t *testing.T) {
	q := &stubQueue{}
	orphan := download.OrphanedAudio{AudioRef: "u/a.mp3", UserId: ids.NewUserId(uuid.New()), TrackId: ids.NewTrackId()}

	if err := downloadfromlibrary.NewOrphanedAudioRecorder(q).RecordOrphanedAudio(context.Background(), orphan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := library.OrphanedAudio{AudioRef: orphan.AudioRef, UserId: orphan.UserId, TrackId: orphan.TrackId}
	if q.got != want {
		t.Errorf("got %+v, want %+v", q.got, want)
	}
}

func TestOrphanedAudioRecorderReturnsQueueError(t *testing.T) {
	boom := errors.New("queue down")
	q := &stubQueue{err: boom}

	err := downloadfromlibrary.NewOrphanedAudioRecorder(q).RecordOrphanedAudio(context.Background(), download.OrphanedAudio{})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}
