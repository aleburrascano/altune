package httputil

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var errTrailingData = errors.New("trailing data after JSON value")

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := decodeSingleValue(r, dst)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{Detail: "request body too large", Code: "request.too_large"})
		return false
	}
	BadRequestCode(w, "request.invalid_body", "invalid request body")
	return false
}

func decodeSingleValue(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errTrailingData
	}
	return nil
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
