package domain

import (
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ValidateSourceURL rejects an acquisition source URL that is oversized, not a
// well-formed http(s) URL, or aimed at a non-public host (see
// validateSourceHost), before it is handed to the acquisition scheduler. An
// empty value is allowed: it signals that no source was supplied.
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

// blockedSourceHostnames are names that always resolve to the server itself or
// to a cloud instance-metadata service.
var blockedSourceHostnames = map[string]bool{
	"localhost":                true,
	"metadata":                 true,
	"metadata.google.internal": true,
	"instance-data":            true,
}

// blockedSourcePrefixes are IP ranges that netip's IsGlobalUnicast/IsPrivate do
// not exclude but that are not publicly routable, or that tunnel to an
// embedded IPv4 address (NAT64, 6to4, IPv4-compatible) which may be internal.
var blockedSourcePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),  // RFC 6598 carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, broadcast
	netip.MustParsePrefix("::/96"),          // IPv4-compatible IPv6
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 well-known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"), // NAT64 local-use prefix
	netip.MustParsePrefix("2002::/16"),      // 6to4
}

// validateSourceHost rejects a source host the server must never fetch on a
// caller's behalf (SSRF / confused deputy): loopback, private, link-local
// (including the 169.254.169.254 metadata endpoint), unspecified, multicast and
// reserved IP literals; localhost and metadata hostnames; and numeric IPv4
// shorthands such as "2130706433" or "0x7f.1" that inet_aton-style resolvers
// expand to internal addresses. It is a syntactic check only: a public name
// that resolves to an internal address must be caught at fetch time.
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

// canonicalSourceHost folds a host the way an IDNA-aware fetcher would before
// resolving it: NFKC (fullwidth "１２７" becomes "127"), the ideographic full
// stop as a label separator, lower case, and no trailing root dot.
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

// isNumericLabel reports whether a final host label is decimal or 0x-hex. No
// real TLD is numeric, so such a host is an IPv4 shorthand that netip refuses
// to parse but system resolvers accept.
func isNumericLabel(label string) bool {
	if hex, ok := strings.CutPrefix(label, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

func errNonPublicSourceHost() error {
	return NewValidationError("track source_url must target a public host")
}
