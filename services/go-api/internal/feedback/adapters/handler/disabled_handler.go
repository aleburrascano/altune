package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// codeFeedbackDisabled tells a client that in-app reports are switched off
// (FEEDBACK_ENABLED=false or no issue tracker configured), distinct from a
// transient tracker failure.
const codeFeedbackDisabled = "feedback.disabled"

type disabledError struct{}

func (disabledError) Error() string     { return "in-app reports are disabled" }
func (disabledError) HTTPStatus() int   { return http.StatusServiceUnavailable }
func (disabledError) ErrorCode() string { return codeFeedbackDisabled }

// DisabledRoutes stands in for Routes when the feedback feature is off: it
// mounts no submit handler and never reaches the issue tracker, answering
// every report with a coded 503 instead of a bare 404.
func DisabledRoutes() chi.Router {
	r := chi.NewRouter()
	r.Post("/reports", func(w http.ResponseWriter, r *http.Request) {
		httputil.HandleServiceError(w, r, disabledError{})
	})
	return r
}
