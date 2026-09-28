package httputil

import (
	"net/http"
	"time"
)

func WriteDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setWriteDeadline(w, time.Now().Add(d))
			next.ServeHTTP(w, r)
		})
	}
}

func ClearWriteDeadline(w http.ResponseWriter) {
	setWriteDeadline(w, time.Time{})
}

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

func (w *idleDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func setWriteDeadline(w http.ResponseWriter, deadline time.Time) {
	_ = http.NewResponseController(w).SetWriteDeadline(deadline)
}
