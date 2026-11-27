package handler

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type sseConn struct {
	rc           *http.ResponseController
	w            http.ResponseWriter
	userId       ids.UserId
	writeTimeout time.Duration
	now          clock.Func
}

func (c *sseConn) writeFrame(frame string) error {
	if c.writeTimeout > 0 {
		if err := c.rc.SetWriteDeadline(c.now().Add(c.writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return fmt.Errorf("set the sse write deadline: %w", err)
		}
	}
	if _, err := io.WriteString(c.w, frame); err != nil {
		return fmt.Errorf("write an sse frame: %w", err)
	}
	if err := c.rc.Flush(); err != nil {
		return fmt.Errorf("flush an sse frame: %w", err)
	}
	return nil
}

func (c *sseConn) writeEvent(ctx context.Context, evt events.Event) error {
	body, err := json.Marshal(evt.Payload)
	if err != nil {
		slog.WarnContext(ctx, "sse.marshal_failed", "event_type", evt.Type, logging.KeyError, err)
		return c.writeResync()
	}
	return c.writeFrame(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, body))
}

func (c *sseConn) writeResync() error {
	return c.writeFrame("event: resync\ndata: {}\n\n")
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}
