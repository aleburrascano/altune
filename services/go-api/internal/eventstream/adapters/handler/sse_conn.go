package handler

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
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
	ctx          context.Context
	rc           *http.ResponseController
	w            http.ResponseWriter
	userId       shared.UserId
	writeTimeout time.Duration
}

func (c *sseConn) writeFrame(frame string) error {
	if c.writeTimeout > 0 {
		if err := c.rc.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	}
	if _, err := io.WriteString(c.w, frame); err != nil {
		return err
	}
	return c.rc.Flush()
}

func (c *sseConn) writeEvent(evt events.Event) error {
	body, err := json.Marshal(evt.Payload)
	if err != nil {
		slog.WarnContext(c.ctx, "sse.marshal_failed", "event_type", evt.Type, "error", err)
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
