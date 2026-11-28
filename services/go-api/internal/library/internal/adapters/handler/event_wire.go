package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
)

type wireEvents struct{ events.Publisher }

func NewWireEvents(next events.Publisher) ports.TrackEvents {
	if next == nil {
		return nil
	}
	return wireEvents{Publisher: next}
}

func (w wireEvents) TrackAdded(ctx context.Context, userId ids.UserId, track *domain.Track) {
	raw, err := json.Marshal(TrackToDTO(track))
	if err != nil {
		slog.ErrorContext(ctx, "library.track_added_event_dropped", logging.KeyError, err)
		return
	}
	w.Publish(ctx, userId, events.TrackAddedToLibrary{TrackID: track.ID().String(), Track: raw})
}
