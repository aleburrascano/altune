package goapi

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// maxEventBytes caps a single SSE frame. A hostile or runaway go-api that never
// terminates a line must not exhaust Overseer's memory: the scanner surfaces an
// over-long frame as an error, which the consumer treats as a dropped stream and
// reconnects, rather than buffering without bound. 1 MiB matches the REST client.
const maxEventBytes = 1 << 20

// Event is a single operator event decoded from go-api's SSE stream. Its fields
// mirror go-api's wire event (internal/admin/eventtap.TapEvent): the type, when
// it happened, and optional user/subject context. Overseer decodes these but
// never acts on them — observe-only.
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
}

// sseDecoder reads a text/event-stream body one frame at a time. go-api emits
// `data: <json>\n\n`; this also tolerates the wider SSE wire form (multi-line
// data, comment lines beginning ":", and non-data fields), ignoring what it does
// not need.
type sseDecoder struct {
	sc   *bufio.Scanner
	data strings.Builder
}

func newSSEDecoder(r io.Reader) *sseDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxEventBytes)
	return &sseDecoder{sc: sc}
}

// next returns the next decoded event, io.EOF when the stream ends cleanly, or a
// read/scan error (including an over-long frame). A malformed JSON frame is
// skipped rather than tearing down the stream — one bad event must not cost the
// resume.
func (d *sseDecoder) next() (Event, error) {
	for d.sc.Scan() {
		line := d.sc.Text()
		if line != "" {
			d.accumulate(line)
			continue
		}
		if ev, ok := d.flush(); ok {
			return ev, nil
		}
	}
	if err := d.sc.Err(); err != nil {
		return Event{}, err
	}
	return Event{}, io.EOF
}

// accumulate folds one non-blank line into the pending frame, keeping only the
// data field. Comment lines (":") and other fields (event/id/retry) are ignored.
func (d *sseDecoder) accumulate(line string) {
	name, value := splitField(line)
	if name != "data" {
		return
	}
	if d.data.Len() > 0 {
		d.data.WriteByte('\n')
	}
	d.data.WriteString(value)
}

// flush decodes and clears the pending frame. ok is false for an empty frame (a
// stray blank line or a comment-only block) or a malformed one, so the caller
// keeps reading instead of emitting a zero event.
func (d *sseDecoder) flush() (Event, bool) {
	raw := d.data.String()
	d.data.Reset()
	if raw == "" {
		return Event{}, false
	}
	var ev Event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return Event{}, false
	}
	return ev, true
}

// splitField parses one SSE line into field name and value, stripping a single
// optional space after the colon per the SSE spec. A line with no colon is a
// bare field name; a leading colon is a comment (empty name).
func splitField(line string) (name, value string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}
	return line[:idx], strings.TrimPrefix(line[idx+1:], " ")
}
