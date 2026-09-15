package httputil

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic.recovered",
					"error", fmt.Sprint(rec),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				InternalError(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
