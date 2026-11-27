package monitoring

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/adapters/persistence"
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/platform/monitoring/internal/shell/clientevents"
	"altune/go-api/internal/shared/clock"
	"fmt"
	"net/http"
)

type (
	ClientEventSink  = handler.ClientEventSink
	ClientEventStore = ports.ClientEventStore
)

var NewPostgresClientEventStore = persistence.NewPostgresClientEventStore

func NewClientEventRoutes(store ClientEventStore, sink ClientEventSink, now clock.Func) (http.Handler, error) {
	recorder, err := clientevents.NewRecorder(store, now)
	if err != nil {
		return nil, fmt.Errorf("client events: %w", err)
	}
	return handler.NewClientEventHandler(recorder, sink, handler.DefaultClientEventLimit, now).Routes(), nil
}
