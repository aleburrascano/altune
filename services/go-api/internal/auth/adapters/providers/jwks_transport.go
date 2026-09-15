package providers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// errInsecureJWKSURL marks a JWKS URL (or redirect target) that is not https.
// The JWKS response decides which public keys Verify trusts, so fetching it in
// plaintext would let a network-positioned attacker substitute the key set and
// mint tokens that verify.
var errInsecureJWKSURL = errors.New("JWKS URL must use https")

// maxJWKSRedirects mirrors net/http's default redirect limit.
const maxJWKSRedirects = 10

// requireSecureJWKSURL accepts an absolute https URL, or plain http only to a
// loopback host (local Supabase, httptest servers): loopback traffic never
// crosses a network an attacker can sit on.
func requireSecureJWKSURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: invalid absolute URL %q", errInsecureJWKSURL, rawURL)
	}
	return requireSecureJWKSTarget(u)
}

func requireSecureJWKSTarget(u *url.URL) error {
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
		return nil
	default:
		return fmt.Errorf("%w (plain http is allowed only for loopback hosts), got scheme %q host %q",
			errInsecureJWKSURL, u.Scheme, u.Hostname())
	}
}

// checkJWKSRedirect is the JWKS client's CheckRedirect: it refuses a redirect
// that would downgrade the fetch to plaintext.
func checkJWKSRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxJWKSRedirects {
		return fmt.Errorf("stopped after %d JWKS redirects", maxJWKSRedirects)
	}
	if err := requireSecureJWKSTarget(req.URL); err != nil {
		return fmt.Errorf("refusing JWKS redirect: %w", err)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
