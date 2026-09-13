package execcmd

import "bytes"

// capWriter buffers writes up to limit bytes and silently discards the rest,
// so a long-running or runaway command cannot grow the capture unbounded (and
// concurrent jobs cannot compound it). It always reports the full write length
// so the command is never killed with a short-write error — output past the
// cap is simply dropped.
type capWriter struct {
	buf   bytes.Buffer
	limit int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) <= room {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:room])
		}
	}
	return len(p), nil
}

func (w *capWriter) String() string { return w.buf.String() }
