package supabase

import (
	"altune/go-api/internal/platform/outbound"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var errInsecureJWKSURL = errors.New("JWKS URL must use https")

const maxJWKSRedirects = 10

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

func checkJWKSRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxJWKSRedirects {
		return fmt.Errorf("stopped after %d JWKS redirects", maxJWKSRedirects)
	}
	if err := requireSecureJWKSTarget(req.URL); err != nil {
		return fmt.Errorf("refusing JWKS redirect: %w", err)
	}
	return nil
}

const maxJWKSBodyBytes = 1 << 20

var ErrJWKSBodyTooLarge = outbound.ErrBodyTooLarge

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
