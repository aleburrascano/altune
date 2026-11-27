package httpserver

import (
	"altune/go-api/internal/shared/apierror"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

var errTrailingData = errors.New("trailing data after JSON value")

const IdempotencyKeyHeader = "Idempotency-Key"

const CacheControlPrivateNoStore = "private, no-store"

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
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errTrailingData
	}
	return nil
}

func URLParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

func QueryInt(w http.ResponseWriter, r *http.Request, name string, def, lo, hi int, invalidCode apierror.Code) (int, bool) {
	value, err := ParseQueryInt(r, name, def, lo, hi, invalidCode)
	if err != nil {
		HandleServiceError(w, r, err)
		return 0, false
	}
	return value, true
}

func ParseQueryInt(r *http.Request, name string, def, lo, hi int, invalidCode apierror.Code) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apierror.NewError(invalidCode, name+" must be an integer")
	}
	if value < lo || value > hi {
		return 0, apierror.NewError(invalidCode, fmt.Sprintf("%s must be between %d and %d", name, lo, hi))
	}
	return value, nil
}

func PathID[T any](w http.ResponseWriter, r *http.Request, name string, parse func(string) (T, error), invalidMsg string) (T, bool) {
	value, err := parse(URLParam(r, name))
	if err != nil {
		HandleServiceError(w, r, apierror.NewError(apierror.CodeRequestInvalidID, invalidMsg))
		var zero T
		return zero, false
	}
	return value, true
}
