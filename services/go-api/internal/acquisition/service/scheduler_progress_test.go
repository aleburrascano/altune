package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type recordingProgressPublisher struct {
	mu     sync.Mutex
	events []recordedProgress
}

type recordedProgress struct {
	typ     string
	payload map[string]any
}

func (p *recordingProgressPublisher) Publish(_ shared.UserId, eventType string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedProgress{typ: eventType, payload: payload})
}

func TestSchedulerJobReporter_PublishesProgressOnStage(t *testing.T) {
	pub := &recordingProgressPublisher{}
	log := &jobLog{jobs: map[string]*ports.JobRecord{"t1": {TrackID: "t1"}}}
	r := schedulerJobReporter{log: log, events: pub, trackID: "t1", userId: shared.NewUserId(uuid.New())}

	r.stage("download")

	if len(pub.events) != 1 {
		t.Fatalf("events = %d, want 1", len(pub.events))
	}
	got := pub.events[0]
	if got.typ != "track_acquisition_progress" {
		t.Fatalf("type = %q, want track_acquisition_progress", got.typ)
	}
	if got.payload["track_id"] != "t1" || got.payload["stage"] != "download" {
		t.Fatalf("payload = %v, want track_id=t1 stage=download", got.payload)
	}
}

func TestSchedulerJobReporter_StageWithoutConfiguredEventsDoesNotPanic(t *testing.T) {
	for name, opts := range map[string][]func(*BackgroundAcquisitionScheduler){
		"no events option":  nil,
		"nil events option": {WithSchedulerEvents(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewBackgroundAcquisitionScheduler(nil, &sync.WaitGroup{}, make(chan struct{}, 1), opts...)
			s.log.register("t1", "")
			r := schedulerJobReporter{log: s.log, events: s.events, trackID: "t1", userId: shared.NewUserId(uuid.New())}

			r.stage("search")

			if s.log.jobs["t1"].Stage != "search" {
				t.Fatalf("stage not recorded on job record")
			}
		})
	}
}
