package httputil

import (
	"altune/go-api/internal/shared/logging"
	"net/http"

	"github.com/google/uuid"
)

const (
	correlationHeader   = "X-Correlation-ID"
	maxCorrelationIDLen = 64
)

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := inboundCorrelationID(r)
		if id == "" {
			id = uuid.New().String()[:8]
		}
		ctx := logging.WithCorrelationID(r.Context(), id)
		w.Header().Set(correlationHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func inboundCorrelationID(r *http.Request) string {
	id := r.Header.Get(correlationHeader)
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
