package memory

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"sync"
)

type RecordingPlaylistPublisher struct {
	mu     sync.Mutex
	Events []events.Payload
}

func (p *RecordingPlaylistPublisher) Publish(_ context.Context, _ ids.UserId, payload events.Payload) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Events = append(p.Events, payload)
}

func (p *RecordingPlaylistPublisher) LastFields(typ events.EventType) map[string]any {
	last := p.Last(typ)
	if last == nil {
		return nil
	}
	raw, err := json.Marshal(last)
	if err != nil {
		return nil
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

func (p *RecordingPlaylistPublisher) Last(typ events.EventType) events.Payload {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.Events) - 1; i >= 0; i-- {
		if p.Events[i].EventType() == typ {
			return p.Events[i]
		}
	}
	return nil
}
