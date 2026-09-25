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

// Bounds on the free text and identifiers the content and enrichment routes
// read. Real titles, names and provider ids stay far below them, while an
// oversized one is spent against MusicBrainz's shared 1 req/s budget, so one
// caller could otherwise exhaust it for every other caller.
const (
	maxTextParamRunes  = 200
	maxExternalIDBytes = 256
)

// textParam reads a trimmed free-text query param, rejecting an oversized one.
func textParam(w http.ResponseWriter, r *http.Request, param string) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(param))
	if utf8.RuneCountInString(value) > maxTextParamRunes {
		httputil.BadRequestCode(w, requestCodeInvalidParam, param+" is too long")
		return "", false
	}
	return value, true
}

// mbidParam reads the optional MusicBrainz id, which must be a canonical UUID:
// it is interpolated into a MusicBrainz path where url.PathEscape keeps "."
// intact, so ".." would address the collection above the entity.
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

// externalIDParam reads the provider's own id for the addressed entity, held
// to the shape that provider's real ids take before it travels into a provider
// URL path.
func externalIDParam(w http.ResponseWriter, r *http.Request, provider domain.ProviderName) (string, bool) {
	externalID := chi.URLParam(r, "externalId")
	if !isValidExternalID(provider, externalID) {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "externalId is not a valid identifier")
		return "", false
	}
	return externalID, true
}

// A "." or ".." id walks out of the entity's path on every provider, since
// url.PathEscape leaves both intact.
func isValidExternalID(provider domain.ProviderName, id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > maxExternalIDBytes {
		return false
	}
	if provider == domain.ProviderLastFM {
		return isArtistRef(id)
	}
	return isOpaqueID(id)
}

// Every provider but last.fm issues opaque ids: digits (deezer, itunes,
// soundcloud, apple music), base62 and "spotify:artist:..." URIs, UUIDs
// (musicbrainz), browse ids (youtube), ASINs (amazon music).
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

// last.fm addresses an artist by name, so its ids carry the spaces, accents
// and percent-escapes an opaque id never does. They are bounded and kept off
// path syntax rather than held to a character set.
func isArtistRef(id string) bool {
	if utf8.RuneCountInString(id) > maxTextParamRunes {
		return false
	}
	hasControlChar := strings.IndexFunc(id, unicode.IsControl) >= 0
	return !hasControlChar && !strings.ContainsAny(id, `/\`)
}

// limitOverflowPolicy names how parseLimit resolves a limit that exceeds max.
type limitOverflowPolicy int

const (
	clampToMax     limitOverflowPolicy = iota // pin an oversized limit to max
	resetToDefault                            // fall back to def instead
)

// parseIntParam reads param as an int, returning def when it is absent and
// false once it has rejected a present but non-numeric value: a mistyped page
// cursor must fail loudly rather than silently serve page one.
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

// limitOrDefault reads param as a positive int, returning def when it is
// absent or non-positive. Any upper bound is the caller's to apply —
// handleSearch relies on this to defer its cap to domain.NewPagedSearchQuery.
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

// parseLimit reads param as a positive limit, applying def when absent or
// non-positive and resolving an over-max value per policy.
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
