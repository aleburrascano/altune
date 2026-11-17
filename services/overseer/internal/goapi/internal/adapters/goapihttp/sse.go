package goapihttp

import (
	"altune/overseer/internal/goapi/internal/core"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

const maxEventBytes = 1 << 20

var ErrFrameTooLarge = errors.New("goapi: sse event frame exceeds max size")

type sseDecoder struct {
	sc   *bufio.Scanner
	data strings.Builder
}

func newSSEDecoder(r io.Reader) *sseDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxEventBytes)
	return &sseDecoder{sc: sc}
}

func (d *sseDecoder) next() (core.Event, error) {
	for d.sc.Scan() {
		line := d.sc.Text()
		if line != "" {
			if err := d.accumulate(line); err != nil {
				return core.Event{}, err
			}
			continue
		}
		if ev, ok := d.flush(); ok {
			return ev, nil
		}
	}
	if err := d.sc.Err(); err != nil {
		return core.Event{}, err
	}
	return core.Event{}, io.EOF
}

func (d *sseDecoder) accumulate(line string) error {
	name, value := splitField(line)
	if name != "data" {
		return nil
	}
	extra := len(value)
	if d.data.Len() > 0 {
		extra++
	}
	if d.data.Len()+extra > maxEventBytes {
		return ErrFrameTooLarge
	}
	if d.data.Len() > 0 {
		d.data.WriteByte('\n')
	}
	d.data.WriteString(value)
	return nil
}

func (d *sseDecoder) flush() (core.Event, bool) {
	raw := d.data.String()
	d.data.Reset()
	if raw == "" {
		return core.Event{}, false
	}
	var ev core.Event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return core.Event{}, false
	}
	ev.CorrID = sanitizeCorrID(ev.CorrID)
	return ev, true
}

func splitField(line string) (name, value string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}
	return line[:idx], strings.TrimPrefix(line[idx+1:], " ")
}

const (
	streamIdleTimeout = 60 * time.Second
	reconnectGrace    = 10 * time.Second
)

var ErrStreamIdle = errors.New("goapi: sse stream silent past idle timeout")

type outage struct {
	since       time.Time
	connectedAt time.Time
}

func (o *outage) connected() { o.connectedAt = time.Now() }

func (o *outage) dropped() core.Status {
	if o.since.IsZero() || time.Since(o.connectedAt) > reconnectGrace {
		o.since = time.Now()
	}
	return o.status()
}

func (o *outage) status() core.Status {
	if o.since.IsZero() || time.Since(o.since) >= reconnectGrace {
		return core.StatusDown
	}
	return core.StatusConnecting
}

type idleWatchdog struct {
	body     io.ReadCloser
	activity chan struct{}
	done     chan struct{}
	silent   atomic.Bool
}

func watchIdle(ctx context.Context, body io.ReadCloser) *idleWatchdog {
	watchdog := &idleWatchdog{body: body, activity: make(chan struct{}, 1), done: make(chan struct{})}
	go watchdog.watch(ctx)
	return watchdog
}

func (w *idleWatchdog) Read(p []byte) (int, error) {
	n, err := w.body.Read(p)
	if n > 0 {
		select {
		case w.activity <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (w *idleWatchdog) watch(ctx context.Context) {
	silence := time.NewTimer(streamIdleTimeout)
	defer silence.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = w.body.Close()
			return
		case <-w.done:
			return
		case <-w.activity:
			silence.Reset(streamIdleTimeout)
		case <-silence.C:
			w.silent.Store(true)
			_ = w.body.Close()
			return
		}
	}
}

func (w *idleWatchdog) stop() { close(w.done) }

func (w *idleWatchdog) cause(readErr error) error {
	if w.silent.Load() {
		return ErrStreamIdle
	}
	return readErr
}

type streamHealth struct {
	status core.Status
	err    error
}

type healthCell struct {
	current atomic.Pointer[streamHealth]
}

func (h *healthCell) load() streamHealth {
	if current := h.current.Load(); current != nil {
		return *current
	}
	return streamHealth{status: core.StatusConnecting}
}

func (h *healthCell) publish(status core.Status, err error) {
	h.current.Store(&streamHealth{status: status, err: err})
}

func (o *outage) untilDown() (time.Duration, bool) {
	if o.since.IsZero() {
		return 0, false
	}
	left := reconnectGrace - time.Since(o.since)
	return left, left > 0
}

func sleepThroughOutage(ctx context.Context, backoff time.Duration, o *outage, h *healthCell) error {
	if backoff <= 0 {
		return ctx.Err()
	}
	retry := time.NewTimer(backoff)
	defer retry.Stop()
	var graceExpired <-chan time.Time
	if left, ok := o.untilDown(); ok && left < backoff {
		grace := time.NewTimer(left)
		defer grace.Stop()
		graceExpired = grace.C
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry.C:
			return nil
		case <-graceExpired:
			graceExpired = nil
			h.publish(o.status(), h.load().err)
		}
	}
}
