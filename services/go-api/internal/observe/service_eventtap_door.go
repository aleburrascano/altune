package observe

import eventtap "altune/go-api/internal/observe/internal/shell/eventtap"

type (
	EventFeed = eventtap.Feed
	EventTap  = eventtap.Tap
)

var (
	ErrFeedStopped        = eventtap.ErrFeedStopped
	ErrTooManySubscribers = eventtap.ErrTooManySubscribers
	NewEventTap           = eventtap.New
	NewEventFeed          = eventtap.NewFeed
)
