package memory

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
)

const emptyTrackBody = "{}"

type trackEvents struct{ events.Publisher }

var _ ports.TrackEvents = trackEvents{}

func NewTrackEvents(next events.Publisher) ports.TrackEvents {
	if next == nil {
		return nil
	}
	return trackEvents{Publisher: next}
}

func (e trackEvents) TrackAdded(ctx context.Context, userId ids.UserId, track *domain.Track) {
	e.Publish(ctx, userId, events.TrackAddedToLibrary{TrackID: track.ID().String(), Track: json.RawMessage(emptyTrackBody)})
}
