package httputil

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := newStatusWriter(w)
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "panic.recovered",
				"error", fmt.Sprint(rec),
				"method", r.Method,
				"path", r.URL.Path,
				"stack", string(debug.Stack()),
			)
			if !sw.hasWrittenHeader {
				InternalError(sw)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
