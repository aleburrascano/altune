package eventstream

import (
	"altune/go-api/internal/platform/eventstream/internal/adapters/handler"
	"errors"
)

var (
	errClockRequired      = errors.New("eventstream: a clock is required")
	errTickerRequired     = errors.New("eventstream: a ticker is required")
	errSubscriberRequired = errors.New("eventstream: an event subscriber is required")
)

type Deps = handler.Config

func New(d Deps) (handler.Routes, error) {
	switch {
	case d.Now == nil:
		return handler.Routes{}, errClockRequired
	case d.NewTicker == nil:
		return handler.Routes{}, errTickerRequired
	case d.Bus == nil:
		return handler.Routes{}, errSubscriberRequired
	}
	return handler.NewRoutes(handler.NewEventStreamHandler(d)), nil
}
