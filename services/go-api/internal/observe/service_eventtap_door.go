package observe

import eventtap "altune/go-api/internal/observe/service/eventtap"

type (
	EventFeed = eventtap.Feed
	EventTap  = eventtap.Tap
)

var (
	NewEventTap  = eventtap.New
	NewEventFeed = eventtap.NewFeed
)
