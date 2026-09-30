package execcmd

import "bytes"

type capWriter struct {
	buf     bytes.Buffer
	limit   int
	dropped bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) <= room {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:room])
			w.dropped = true
		}
	} else if len(p) > 0 {
		w.dropped = true
	}
	return len(p), nil
}

func (w *capWriter) String() string { return w.buf.String() }
