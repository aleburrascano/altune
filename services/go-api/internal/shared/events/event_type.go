package events

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"time"
)

type EventType string

const (
	TypeTrackAddedToLibrary      EventType = "track_added_to_library"
	typeTrackDeleted             EventType = "track_deleted"
	TypeTrackAcquisitionStarted  EventType = "track_acquisition_started"
	TypeTrackAcquisitionProgress EventType = "track_acquisition_progress"

	typeTrackAcquisitionCompleted EventType = "track_acquisition_completed"
	TypeTrackAcquisitionFailed    EventType = "track_acquisition_failed"
	TypeTrackReplaceFailed        EventType = "track_replace_failed"

	typeTrackAddedToPlaylist      EventType = "track_added_to_playlist"
	typeTracksAddedToPlaylist     EventType = "tracks_added_to_playlist"
	typeTrackRemovedFromPlaylist  EventType = "track_removed_from_playlist"
	typeTracksRemovedFromPlaylist EventType = "tracks_removed_from_playlist"

	typePlaylistCreated   EventType = "playlist_created"
	typePlaylistDeleted   EventType = "playlist_deleted"
	TypePlaylistRenamed   EventType = "playlist_renamed"
	typePlaylistReordered EventType = "playlist_reordered"
)

type Event struct {
	ID        uint64     `json:"id"`
	Type      EventType  `json:"type"`
	UserID    ids.UserId `json:"-"`
	Payload   Payload    `json:"payload"`
	Timestamp time.Time  `json:"timestamp"`
}

type Publisher interface {
	Publish(ctx context.Context, userId ids.UserId, payload Payload)
}

func NoopPublisher() Publisher { return noopPublisher{} }

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, ids.UserId, Payload) {}
