package monitoring

import eventtap "altune/go-api/internal/platform/monitoring/internal/shell/eventwatch"

type (
	EventFeed = eventtap.WatchedEvents
	EventTap  = eventtap.EventWatcher
)

var (
	ErrFeedStopped        = eventtap.ErrFeedStopped
	ErrTooManySubscribers = eventtap.ErrTooManySubscribers
	NewEventTap           = eventtap.New
	NewEventFeed          = eventtap.NewFeed
)
