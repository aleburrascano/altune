package domain

import (
	"time"

	"altune/go-api/internal/shared"
)

type TrackAddedToLibrary struct {
	OccurredAt time.Time
	TrackId    TrackId
	UserId     shared.UserId
}

type PlaylistCreated struct {
	OccurredAt time.Time
	PlaylistId PlaylistId
	UserId     shared.UserId
	Name       string
}

type PlaylistDeleted struct {
	OccurredAt time.Time
	PlaylistId PlaylistId
	UserId     shared.UserId
}

type TrackAddedToPlaylist struct {
	OccurredAt time.Time
	PlaylistId PlaylistId
	TrackId    TrackId
	UserId     shared.UserId
}

type TrackRemovedFromPlaylist struct {
	OccurredAt time.Time
	PlaylistId PlaylistId
	TrackId    TrackId
	UserId     shared.UserId
}

type TrackAcquisitionCompleted struct {
	OccurredAt time.Time
	TrackId    TrackId
	UserId     shared.UserId
	AudioRef   string
}

type TrackAcquisitionFailed struct {
	OccurredAt time.Time
	TrackId    TrackId
	UserId     shared.UserId
	Reason     string
}
