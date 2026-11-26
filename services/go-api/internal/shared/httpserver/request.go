package httpserver

import (
	"altune/go-api/internal/shared/apierror"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

var errTrailingData = errors.New("trailing data after JSON value")

const IdempotencyKeyHeader = "Idempotency-Key"

func IdempotencyKey(r *http.Request) *string {
	key := strings.TrimSpace(r.Header.Get(IdempotencyKeyHeader))
	if key == "" {
		return nil
	}
	return &key
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return DecodeJSONWithCode(w, r, dst, apierror.CodeRequestInvalid)
}

func DecodeJSONWithCode(w http.ResponseWriter, r *http.Request, dst any, invalidCode apierror.Code) bool {
	err := decodeSingleValue(r, dst)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		HandleServiceError(w, r, apierror.NewError(apierror.CodeRequestTooLarge, "request body too large"))
		return false
	}
	HandleServiceError(w, r, apierror.NewError(invalidCode, "invalid request body"))
	return false
}

func decodeSingleValue(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errTrailingData
	}
	return nil
}

func PathID[T any](w http.ResponseWriter, r *http.Request, name string, parse func(string) (T, error), invalidMsg string) (T, bool) {
	value, err := parse(chi.URLParam(r, name))
	if err != nil {
		HandleServiceError(w, r, apierror.NewError(apierror.CodeRequestInvalidID, invalidMsg))
		var zero T
		return zero, false
	}
	return value, true
}
