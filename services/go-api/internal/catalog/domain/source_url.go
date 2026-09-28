package domain

import (
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func ValidateSourceURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > maxTrackTextLength {
		return trackTextTooLongError("source_url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return NewValidationError("track source_url is malformed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return NewValidationError("track source_url must be an http or https URL")
	}
	if parsed.Hostname() == "" {
		return NewValidationError("track source_url must include a host")
	}
	return validateSourceHost(parsed.Hostname())
}

var blockedSourceHostnames = map[string]bool{
	"localhost":                true,
	"metadata":                 true,
	"metadata.google.internal": true,
	"instance-data":            true,
}

var blockedSourcePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

func validateSourceHost(host string) error {
	host = canonicalSourceHost(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		return validateSourceAddr(addr)
	}
	if isBlockedSourceHostname(host) {
		return errNonPublicSourceHost()
	}
	return nil
}

func canonicalSourceHost(host string) string {
	host = norm.NFKC.String(host)
	host = strings.ReplaceAll(host, "。", ".")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func validateSourceAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	if addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() || inBlockedSourcePrefix(addr) {
		return errNonPublicSourceHost()
	}
	return nil
}

func inBlockedSourcePrefix(addr netip.Addr) bool {
	for _, prefix := range blockedSourcePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func isBlockedSourceHostname(host string) bool {
	if host == "" || blockedSourceHostnames[host] || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return isNumericLabel(host[strings.LastIndex(host, ".")+1:])
}

func isNumericLabel(label string) bool {
	if hex, ok := strings.CutPrefix(label, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

func errNonPublicSourceHost() error {
	return NewValidationError("track source_url must target a public host")
}
