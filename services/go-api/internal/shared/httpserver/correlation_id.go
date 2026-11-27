package httpserver

import (
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"net/http"
)

const (
	CorrelationIDHeader = "X-Correlation-ID"
	maxCorrelationIDLen = 64
)

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := inboundCorrelationID(r)
		if id == "" {
			id = ids.Real().String()[:8]
		}
		ctx := logging.ContextWithCorrelationID(r.Context(), id)
		w.Header().Set(CorrelationIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func inboundCorrelationID(r *http.Request) string {
	id := r.Header.Get(CorrelationIDHeader)
	if id == "" || len(id) > maxCorrelationIDLen || !isWellFormedCorrelationID(id) {
		return ""
	}
	return id
}

func isWellFormedCorrelationID(id string) bool {
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}
