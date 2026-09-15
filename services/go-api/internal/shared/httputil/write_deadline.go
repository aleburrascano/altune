package httputil

import (
	"net/http"
	"time"
)

// WriteDeadline bounds how long a response may take to write: every request
// gets a connection write deadline of now+d, so a client that stops reading
// cannot hold the handler goroutine blocked in Write forever. It is the
// per-route counterpart of http.Server.WriteTimeout, applied as middleware so
// long-lived handlers can opt out (ClearWriteDeadline) or switch to a
// progress-based deadline (ExtendWriteDeadlineOnWrite). net/http clears the
// deadline once the response completes, so it never leaks to the next request
// on a keep-alive connection.
func WriteDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setWriteDeadline(w, time.Now().Add(d))
			next.ServeHTTP(w, r)
		})
	}
}

// ClearWriteDeadline removes the route-level write deadline for a long-lived
// stream (SSE) that is expected to outlive it.
func ClearWriteDeadline(w http.ResponseWriter) {
	setWriteDeadline(w, time.Time{})
}

// ExtendWriteDeadlineOnWrite returns a writer that pushes the write deadline to
// now+idle before every Write. The response may take as long as it needs while
// the client keeps reading, but a client that stalls for longer than idle is cut
// off. Use it for large bodies (audio) served to legitimately slow networks.
func ExtendWriteDeadlineOnWrite(w http.ResponseWriter, idle time.Duration) http.ResponseWriter {
	return &idleDeadlineWriter{ResponseWriter: w, idle: idle}
}

type idleDeadlineWriter struct {
	http.ResponseWriter
	idle time.Duration
}

func (w *idleDeadlineWriter) Write(b []byte) (int, error) {
	setWriteDeadline(w.ResponseWriter, time.Now().Add(w.idle))
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying connection.
func (w *idleDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// setWriteDeadline applies deadline through http.ResponseController. The error
// is deliberately dropped: a writer that cannot reach a connection (e.g.
// httptest.ResponseRecorder) has nothing to bound, and any other failure means
// the connection is already unusable, which the next Write surfaces.
func setWriteDeadline(w http.ResponseWriter, deadline time.Time) {
	_ = http.NewResponseController(w).SetWriteDeadline(deadline)
}
