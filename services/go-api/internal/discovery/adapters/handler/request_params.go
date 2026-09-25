package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxTextParamRunes  = 200
	maxExternalIDBytes = 256
)

func textParam(w http.ResponseWriter, r *http.Request, param string) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(param))
	if utf8.RuneCountInString(value) > maxTextParamRunes {
		httputil.BadRequestCode(w, requestCodeInvalidParam, param+" is too long")
		return "", false
	}
	return value, true
}

func mbidParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	mbid := strings.TrimSpace(r.URL.Query().Get("mbid"))
	if mbid == "" || isCanonicalUUID(mbid) {
		return mbid, true
	}
	httputil.BadRequestCode(w, requestCodeInvalidParam, "mbid must be a UUID")
	return "", false
}

func isCanonicalUUID(s string) bool {
	parsed, err := uuid.Parse(s)
	return err == nil && strings.EqualFold(parsed.String(), s)
}

func externalIDParam(w http.ResponseWriter, r *http.Request, provider domain.ProviderName) (string, bool) {
	externalID := chi.URLParam(r, "externalId")
	if !isValidExternalID(provider, externalID) {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "externalId is not a valid identifier")
		return "", false
	}
	return externalID, true
}

func isValidExternalID(provider domain.ProviderName, id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > maxExternalIDBytes {
		return false
	}
	if provider == domain.ProviderLastFM {
		return isArtistRef(id)
	}
	return isOpaqueID(id)
}

func isOpaqueID(id string) bool {
	for _, c := range id {
		if !isOpaqueIDChar(c) {
			return false
		}
	}
	return true
}

func isOpaqueIDChar(c rune) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	return strings.ContainsRune("_:.-", c)
}

func isArtistRef(id string) bool {
	if utf8.RuneCountInString(id) > maxTextParamRunes {
		return false
	}
	hasControlChar := strings.IndexFunc(id, unicode.IsControl) >= 0
	return !hasControlChar && !strings.ContainsAny(id, `/\`)
}

type limitOverflowPolicy int

const (
	clampToMax limitOverflowPolicy = iota
	resetToDefault
)

func parseIntParam(w http.ResponseWriter, r *http.Request, param string, def int) (int, bool) {
	raw := r.URL.Query().Get(param)
	if raw == "" {
		return def, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		httputil.BadRequestCode(w, requestCodeInvalidParam, param+" must be an integer")
		return 0, false
	}
	return value, true
}

func limitOrDefault(w http.ResponseWriter, r *http.Request, param string, def int) (int, bool) {
	limit, ok := parseIntParam(w, r, param, def)
	if !ok {
		return 0, false
	}
	if limit <= 0 {
		return def, true
	}
	return limit, true
}

func parseLimit(w http.ResponseWriter, r *http.Request, param string, def, maxLimit int, policy limitOverflowPolicy) (int, bool) {
	limit, ok := limitOrDefault(w, r, param, def)
	if !ok {
		return 0, false
	}
	if limit <= maxLimit {
		return limit, true
	}
	if policy == resetToDefault {
		return def, true
	}
	return maxLimit, true
}
