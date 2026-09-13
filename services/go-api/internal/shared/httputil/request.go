package httputil

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DecodeJSON decodes the JSON request body into dst. On failure it writes a
// 400 Bad Request with the detail "invalid request body" and returns false, so
// callers can guard with `if !DecodeJSON(w, r, &body) { return }`.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		BadRequest(w, "invalid request body")
		return false
	}
	return true
}

// PathID reads the chi URL param named `name`, parses it with `parse`, and on
// failure writes a 400 Bad Request with `invalidMsg` and returns false. The
// message is passed explicitly so each caller keeps its exact wording (e.g.
// "invalid track ID" vs "invalid playlist ID").
func PathID[T any](w http.ResponseWriter, r *http.Request, name string, parse func(string) (T, error), invalidMsg string) (T, bool) {
	value, err := parse(chi.URLParam(r, name))
	if err != nil {
		BadRequest(w, invalidMsg)
		var zero T
		return zero, false
	}
	return value, true
}
