package httputil

import (
	"altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/logging"
	"errors"
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
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "panic.recovered",
				logging.KeyError, fmt.Sprint(rec),
				logging.KeyMethod, r.Method,
				logging.KeyPath, r.URL.Path,
				logging.KeyStack, string(debug.Stack()),
			)
			if !sw.hasWrittenHeader {
				writeProblem(sw, newProblem(r, domain.CodeInternal, internalServerErrorDetail))
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
