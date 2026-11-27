package eventstest

import (
	"altune/go-api/internal/shared/events"
	"encoding/json"
	"fmt"
)

type Event struct {
	Name   events.EventType
	Fields map[string]any
}

func Named(name events.EventType) Event {
	return Event{Name: name}
}

func Fielded(name events.EventType, fields map[string]any) Event {
	return Event{Name: name, Fields: fields}
}

func (e Event) EventType() events.EventType { return e.Name }

func (e Event) MarshalJSON() ([]byte, error) {
	r0, err := json.Marshal(e.Fields)
	if err != nil {
		return r0, fmt.Errorf("marshal: %w", err)
	}
	return r0, nil
}

func (e Event) Subject() string {
	for _, key := range []string{"track_id", "entity_id", "result_signature"} {
		if s, ok := e.Fields[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
