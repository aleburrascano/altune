package events

import (
	"time"

	"altune/go-api/internal/shared"
)

// The event types publishers pass to Publish. The values are the wire contract
// with subscribed clients (apps/mobile/src/shared/events/eventTypes.ts keeps the
// matching list), so changing one breaks every client already deployed.
const (
	TypeTrackAddedToLibrary      = "track_added_to_library"
	TypeTrackDeleted             = "track_deleted"
	TypeTrackAcquisitionStarted  = "track_acquisition_started"
	TypeTrackAcquisitionProgress = "track_acquisition_progress"

	TypeTrackAcquisitionCompleted = "track_acquisition_completed"
	TypeTrackAcquisitionFailed    = "track_acquisition_failed"
	TypeTrackReplaceFailed        = "track_replace_failed"

	TypeTrackAddedToPlaylist      = "track_added_to_playlist"
	TypeTracksAddedToPlaylist     = "tracks_added_to_playlist"
	TypeTrackRemovedFromPlaylist  = "track_removed_from_playlist"
	TypeTracksRemovedFromPlaylist = "tracks_removed_from_playlist"

	TypePlaylistCreated   = "playlist_created"
	TypePlaylistDeleted   = "playlist_deleted"
	TypePlaylistRenamed   = "playlist_renamed"
	TypePlaylistReordered = "playlist_reordered"
)

type Event struct {
	ID        uint64         `json:"id"`
	Type      string         `json:"type"`
	UserID    shared.UserId  `json:"-"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
}

type Publisher interface {
	Publish(userId shared.UserId, eventType string, payload map[string]any)
}

func NoopPublisher() Publisher { return noopPublisher{} }

type noopPublisher struct{}

func (noopPublisher) Publish(shared.UserId, string, map[string]any) {}

type Subscriber interface {
	Subscribe(userId shared.UserId) (ch <-chan Event, cancel func())
	Replay(userId shared.UserId, afterID uint64) []Event
	// HighestIssuedID is the largest event ID this process could have issued to
	// any client. A resume ID above it was never issued here (stale or corrupt)
	// and cannot be replayed from.
	HighestIssuedID() uint64
}
