package httputil

import (
	"altune/go-api/internal/shared/domain"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var errTrailingData = errors.New("trailing data after JSON value")

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return DecodeJSONWithCode(w, r, dst, domain.CodeRequestInvalid)
}

func DecodeJSONWithCode(w http.ResponseWriter, r *http.Request, dst any, invalidCode domain.Code) bool {
	err := decodeSingleValue(r, dst)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		HandleServiceError(w, r, domain.NewError(domain.CodeRequestTooLarge, "request body too large"))
		return false
	}
	HandleServiceError(w, r, domain.NewError(invalidCode, "invalid request body"))
	return false
}

func decodeSingleValue(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errTrailingData
	}
	return nil
}

func PathID[T any](w http.ResponseWriter, r *http.Request, name string, parse func(string) (T, error), invalidMsg string) (T, bool) {
	value, err := parse(chi.URLParam(r, name))
	if err != nil {
		HandleServiceError(w, r, domain.NewError(domain.CodeRequestInvalidID, invalidMsg))
		var zero T
		return zero, false
	}
	return value, true
}
