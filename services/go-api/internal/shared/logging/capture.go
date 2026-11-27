package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"sync"
)

type tb interface {
	Helper()
	Cleanup(func())
}

type Capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r0, err := c.buf.Write(p)
	if err != nil {
		return r0, fmt.Errorf("write: %w", err)
	}
	return r0, nil
}

func (c *Capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *Capture) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Len()
}

func (c *Capture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Reset()
}

func CaptureText(t tb, level slog.Level) *Capture {
	t.Helper()
	c := &Capture{}
	UseHandler(t, newCorrelationHandler(slog.NewTextHandler(c, &slog.HandlerOptions{Level: level})))
	return c
}

func CaptureJSON(t tb, level slog.Level) *Capture {
	t.Helper()
	c := &Capture{}
	UseHandler(t, newCorrelationHandler(slog.NewJSONHandler(c, &slog.HandlerOptions{Level: level})))
	return c
}

func CaptureRing(t tb, level string) *RingBuffer {
	t.Helper()
	t.Cleanup(Swap(slog.Default()))
	return Setup(level)
}

func Silence(t tb) {
	t.Helper()
	UseHandler(t, slog.DiscardHandler)
}

func UseHandler(t tb, h slog.Handler) {
	t.Helper()
	t.Cleanup(Swap(slog.New(h)))
}
