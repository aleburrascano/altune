package observer

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/httpserver"
	"net/http"
	"time"
)

func New(record func(route string, d time.Duration, status int)) httpserver.Observer {
	return func(r *http.Request) (*http.Request, func(httpserver.Outcome)) {
		return r, func(o httpserver.Outcome) {
			defer background.RecoverContext(r.Context(), "reqmetrics.record_panic")
			record(o.Route, o.Duration, o.Status)
		}
	}
}
