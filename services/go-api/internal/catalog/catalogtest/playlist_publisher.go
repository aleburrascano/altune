package catalogtest

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"sync"
)

type RecordingPlaylistPublisher struct {
	mu     sync.Mutex
	Events []struct {
		typ     string
		payload map[string]any
	}
}

func (p *RecordingPlaylistPublisher) Publish(_ context.Context, _ shared.UserId, eventType events.EventType, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Events = append(p.Events, struct {
		typ     string
		payload map[string]any
	}{string(eventType), payload})
}

func (p *RecordingPlaylistPublisher) Last(typ events.EventType) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.Events) - 1; i >= 0; i-- {
		if p.Events[i].typ == string(typ) {
			return p.Events[i].payload
		}
	}
	return nil
}
