package handler

import (
	"altune/go-api/internal/search/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

func invalidParam(detail string) error {
	return apierror.NewError(apierror.CodeDiscoveryInvalidParam, detail)
}

const (
	maxTextParamRunes  = 200
	maxExternalIDBytes = 256
)

func textParam(w http.ResponseWriter, r *http.Request, param string) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(param))
	if utf8.RuneCountInString(value) > maxTextParamRunes {
		httpserver.HandleServiceError(w, r, invalidParam(param+" is too long"))
		return "", false
	}
	return value, true
}

const maxKnownIDs = 16

func knownIDsParam(r *http.Request) map[domain.ProviderName]string {
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	if raw == "" {
		return nil
	}
	known := make(map[domain.ProviderName]string)
	for i, entry := range strings.Split(raw, ",") {
		if i == maxKnownIDs {
			break
		}
		provider, id, found := strings.Cut(strings.TrimSpace(entry), ":")
		pn, err := domain.ParseProviderName(provider)
		if found && err == nil && isValidExternalID(id) {
			known[pn] = id
		}
	}
	return known
}

func mbidParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	mbid := strings.TrimSpace(r.URL.Query().Get("mbid"))
	if mbid == "" || isCanonicalUUID(mbid) {
		return mbid, true
	}
	httpserver.HandleServiceError(w, r, invalidParam("mbid must be a UUID"))
	return "", false
}

func isCanonicalUUID(s string) bool {
	parsed, err := uuid.Parse(s)
	return err == nil && strings.EqualFold(parsed.String(), s)
}

func externalIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	externalID := httpserver.URLParam(r, "externalId")
	if !isValidExternalID(externalID) {
		httpserver.HandleServiceError(w, r, invalidParam("externalId is not a valid identifier"))
		return "", false
	}
	return externalID, true
}

func isValidExternalID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > maxExternalIDBytes {
		return false
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

func parseIntParam(w http.ResponseWriter, r *http.Request, param string, def int) (int, bool) {
	return httpserver.QueryInt(w, r, param, def, math.MinInt, math.MaxInt, apierror.CodeDiscoveryInvalidParam)
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

func parseLimit(w http.ResponseWriter, r *http.Request, param string, def, maxLimit int) (int, bool) {
	limit, ok := limitOrDefault(w, r, param, def)
	if !ok {
		return 0, false
	}
	if limit > maxLimit {
		httpserver.HandleServiceError(w, r, invalidParam(fmt.Sprintf("%s must be between 1 and %d", param, maxLimit)))
		return 0, false
	}
	return limit, true
}
