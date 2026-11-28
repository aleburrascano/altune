package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
)

type wireEvents struct{ next events.Publisher }

func NewWireEvents(next events.Publisher) events.Publisher {
	if next == nil {
		return nil
	}
	return wireEvents{next: next}
}

func (w wireEvents) Publish(ctx context.Context, userId ids.UserId, payload events.Payload) {
	switch added := payload.(type) {
	case events.TrackAddedToLibrary:
		if track, ok := added.Track.(*domain.Track); ok {
			added.Track = TrackToDTO(track)
			payload = added
		}
	}
	w.next.Publish(ctx, userId, payload)
}
