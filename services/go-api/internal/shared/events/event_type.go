package events

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type EventType string

const (
	TypeTrackAddedToLibrary      EventType = "track_added_to_library"
	TypeTrackDeleted             EventType = "track_deleted"
	TypeTrackAcquisitionStarted  EventType = "track_acquisition_started"
	TypeTrackAcquisitionProgress EventType = "track_acquisition_progress"

	TypeTrackAcquisitionCompleted EventType = "track_acquisition_completed"
	TypeTrackAcquisitionFailed    EventType = "track_acquisition_failed"
	TypeTrackReplaceFailed        EventType = "track_replace_failed"

	TypeTrackAddedToPlaylist      EventType = "track_added_to_playlist"
	TypeTracksAddedToPlaylist     EventType = "tracks_added_to_playlist"
	TypeTrackRemovedFromPlaylist  EventType = "track_removed_from_playlist"
	TypeTracksRemovedFromPlaylist EventType = "tracks_removed_from_playlist"

	TypePlaylistCreated   EventType = "playlist_created"
	TypePlaylistDeleted   EventType = "playlist_deleted"
	TypePlaylistRenamed   EventType = "playlist_renamed"
	TypePlaylistReordered EventType = "playlist_reordered"
)

type Event struct {
	ID        uint64         `json:"id"`
	Type      EventType      `json:"type"`
	UserID    shared.UserId  `json:"-"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
}

type Publisher interface {
	Publish(ctx context.Context, userId shared.UserId, eventType EventType, payload map[string]any)
}

func NoopPublisher() Publisher { return noopPublisher{} }

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, shared.UserId, EventType, map[string]any) {}

type Subscriber interface {
	Subscribe(userId shared.UserId) (ch <-chan Event, cancel func())
	Replay(userId shared.UserId, afterID uint64) []Event
	ResumeGapped(userId shared.UserId, afterID uint64) bool
	HighestIssuedID() uint64
	LatestID(userId shared.UserId) uint64
}
