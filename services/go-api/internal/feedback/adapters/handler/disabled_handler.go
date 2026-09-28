package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const codeFeedbackDisabled = "feedback.disabled"

type disabledError struct{}

func (disabledError) Error() string     { return "in-app reports are disabled" }
func (disabledError) HTTPStatus() int   { return http.StatusServiceUnavailable }
func (disabledError) ErrorCode() string { return codeFeedbackDisabled }

func DisabledRoutes() chi.Router {
	r := chi.NewRouter()
	r.Post("/reports", func(w http.ResponseWriter, r *http.Request) {
		httputil.HandleServiceError(w, r, disabledError{})
	})
	return r
}
