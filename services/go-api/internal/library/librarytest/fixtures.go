package librarytest

import (
	"altune/go-api/internal/library/internal/adapters/memory/fixturestest"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
)

var (
	OtherUserId    = fixturestest.OtherUserId
	SeedPlaylist   = fixturestest.SeedPlaylist
	SeedReadyTrack = fixturestest.SeedReadyTrack
	SeedTrack      = fixturestest.SeedTrack
	UserId         = fixturestest.UserId
)

func ActingAs(userId ids.UserId) context.Context {
	return logging.ContextWithRequestFields(context.Background(), func() logging.RequestFields {
		return logging.RequestFields{UserID: userId.String()}
	})
}

func NewTrackId() ids.TrackId { return ids.Source(ids.Real).NewTrackId() }

func NewPlaylistId() domain.PlaylistId {
	return domain.PlaylistId{EntityId: ids.Source(ids.Real).NewEntityId()}
}
