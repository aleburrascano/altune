package retry

import (
	"altune/go-api/internal/shared/retrysafe"
	"net/http"
)

func mayRetry(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	}
	return retrysafe.Marked(req.Context())
}
