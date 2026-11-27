package logging

import (
	"bytes"
	"log/slog"
	"sync"
)

type TB interface {
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
	return c.buf.Write(p)
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

func CaptureText(t TB, level slog.Level) *Capture {
	t.Helper()
	c := &Capture{}
	UseHandler(t, slog.NewTextHandler(c, &slog.HandlerOptions{Level: level}))
	return c
}

func CaptureJSON(t TB, level slog.Level) *Capture {
	t.Helper()
	c := &Capture{}
	UseHandler(t, slog.NewJSONHandler(c, &slog.HandlerOptions{Level: level}))
	return c
}

func CaptureRing(t TB, level string) *RingBuffer {
	t.Helper()
	t.Cleanup(Swap(slog.Default()))
	return Setup(level)
}

func Silence(t TB) {
	t.Helper()
	UseHandler(t, slog.DiscardHandler)
}

func UseHandler(t TB, h slog.Handler) {
	t.Helper()
	t.Cleanup(Swap(slog.New(h)))
}
