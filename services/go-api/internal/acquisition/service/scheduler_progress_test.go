package service

import (
	"sync"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/shared"
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
	log := &jobLog{jobs: map[string]*JobRecord{"t1": {TrackID: "t1"}}}
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

func TestSchedulerJobReporter_NoPublishWhenEventsNil(t *testing.T) {
	log := &jobLog{jobs: map[string]*JobRecord{"t1": {TrackID: "t1"}}}
	r := schedulerJobReporter{log: log, trackID: "t1", userId: shared.NewUserId(uuid.New())} // events nil — eval/test path

	r.stage("search") // must not panic

	if log.jobs["t1"].Stage != "search" {
		t.Fatalf("stage not recorded on job record")
	}
}
