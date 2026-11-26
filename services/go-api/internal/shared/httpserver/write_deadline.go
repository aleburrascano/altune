package httpserver

import (
	"altune/go-api/internal/shared/clock"
	"net/http"
	"time"
)

func WriteDeadline(now clock.Func, d time.Duration) func(http.Handler) http.Handler {
	requireClock("WriteDeadline", now)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setWriteDeadline(w, now().Add(d))
			next.ServeHTTP(w, r)
		})
	}
}

func ClearWriteDeadline(w http.ResponseWriter) {
	setWriteDeadline(w, time.Time{})
}

func ExtendWriteDeadlineOnWrite(now clock.Func, w http.ResponseWriter, idle time.Duration) http.ResponseWriter {
	requireClock("ExtendWriteDeadlineOnWrite", now)
	return &idleDeadlineWriter{ResponseWriter: w, now: now, idle: idle}
}

type idleDeadlineWriter struct {
	http.ResponseWriter
	now  clock.Func
	idle time.Duration
}

func (w *idleDeadlineWriter) Write(b []byte) (int, error) {
	setWriteDeadline(w.ResponseWriter, w.now().Add(w.idle))
	return w.ResponseWriter.Write(b)
}

func (w *idleDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requireClock(fn string, now clock.Func) {
	if now == nil {
		panic("httpserver." + fn + ": a nil clock")
	}
}

func setWriteDeadline(w http.ResponseWriter, deadline time.Time) {
	_ = http.NewResponseController(w).SetWriteDeadline(deadline)
}
