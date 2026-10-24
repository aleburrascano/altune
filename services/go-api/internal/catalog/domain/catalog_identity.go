package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	MaxCatalogSources        = 16
	maxCatalogExternalIDSize = 200
	canonicalUUIDLength      = 36
)

var knownCatalogProviders = map[string]struct{}{
	"deezer":      {},
	"musicbrainz": {},
	"soundcloud":  {},
	"lastfm":      {},
	"itunes":      {},
	"theaudiodb":  {},
	"discogs":     {},
	"youtube":     {},
	"amazonmusic": {},
	"applemusic":  {},
	"spotify":     {},
}

const PixeldrainProvider = "pixeldrain"

var pinOnlyCatalogProviders = map[string]*regexp.Regexp{
	PixeldrainProvider: regexp.MustCompile(`^[A-Za-z0-9]{6,16}$`),
}

func KnownCatalogProviders() []string {
	names := make([]string, 0, len(knownCatalogProviders))
	for name := range knownCatalogProviders {
		names = append(names, name)
	}
	return names
}

type CatalogSource struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url,omitempty"`
}

func WithSoundCloudURL(sources []CatalogSource, rawURL string) []CatalogSource {
	rawURL = strings.TrimSpace(rawURL)
	if !isSoundCloudURL(rawURL) {
		return sources
	}
	out := append([]CatalogSource(nil), sources...)
	for i := range out {
		if out[i].Provider == "soundcloud" {
			out[i].URL = rawURL
			return out
		}
	}
	if len(rawURL) > maxCatalogExternalIDSize || len(out) >= MaxCatalogSources {
		return sources
	}
	return append(out, CatalogSource{Provider: "soundcloud", ExternalID: rawURL, URL: rawURL})
}

func isSoundCloudURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com")
}

var uuidDashPositions = map[int]bool{8: true, 13: true, 18: true, 23: true}

func isCanonicalUUID(raw string) bool {
	if len(raw) != canonicalUUIDLength {
		return false
	}
	for i, r := range raw {
		if uuidDashPositions[i] != (r == '-') {
			return false
		}
		if r != '-' && !isHexDigit(r) {
			return false
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func NormalizeCatalogMBID(raw string) (string, error) {
	if !isCanonicalUUID(raw) {
		return "", NewValidationError("mbid must be a UUID")
	}
	return strings.ToLower(raw), nil
}

func validateCatalogProvider(s CatalogSource) error {
	if _, known := knownCatalogProviders[s.Provider]; known {
		return nil
	}
	pinID, pinOnly := pinOnlyCatalogProviders[s.Provider]
	if !pinOnly {
		return NewValidationError("sources provider is unknown: " + s.Provider)
	}
	if !pinID.MatchString(s.ExternalID) {
		return NewValidationError("sources external_id is not a valid " + s.Provider + " id")
	}
	return nil
}

func validateCatalogSource(s CatalogSource) error {
	if err := validateCatalogProvider(s); err != nil {
		return err
	}
	if s.ExternalID == "" {
		return NewValidationError("sources external_id must not be empty")
	}
	if strings.ContainsRune(s.ExternalID, '\x00') {
		return NewValidationError("sources external_id must not contain a NUL byte")
	}
	if len(s.ExternalID) > maxCatalogExternalIDSize {
		return NewValidationError(fmt.Sprintf("sources external_id exceeds %d characters", maxCatalogExternalIDSize))
	}
	return ValidateSourceURL(s.URL)
}

func NormalizeCatalogSources(sources []CatalogSource) ([]CatalogSource, error) {
	if len(sources) > MaxCatalogSources {
		return nil, NewValidationError(fmt.Sprintf("sources exceeds %d entries", MaxCatalogSources))
	}
	seen := make(map[CatalogSource]struct{}, len(sources))
	unique := make([]CatalogSource, 0, len(sources))
	for _, s := range sources {
		if err := validateCatalogSource(s); err != nil {
			return nil, err
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		unique = append(unique, s)
	}
	return unique, nil
}
