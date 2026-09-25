package providers

import (
	"errors"
	"fmt"
	"io"
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

// maxJWKSBodyBytes caps how much of a JWKS response the verifier will buffer.
// A real Supabase key set is a few hundred bytes, so 1 MiB is far above any
// legitimate one. The bound is needed because the fetcher reads the body with
// io.ReadAll: without it, an endpoint or a middle hop that streams for the
// whole jwksFetchTimeout could exhaust the process's memory, on a path an
// unauthenticated caller can nudge with unknown-kid tokens.
const maxJWKSBodyBytes = 1 << 20

// errJWKSBodyTooLarge marks a JWKS response longer than maxJWKSBodyBytes. It
// is an error rather than a truncation because a truncated body can still
// parse: a valid key set followed by padding would be accepted as a fetch the
// cap was supposed to refuse.
var errJWKSBodyTooLarge = fmt.Errorf("JWKS response body exceeds %d bytes", maxJWKSBodyBytes)

// cappedJWKSBodyTransport is the JWKS client's RoundTripper: it caps every
// response body, the one place a size limit can be imposed without changing
// how the jwx cache reads it.
type cappedJWKSBodyTransport struct{ base http.RoundTripper }

func (t cappedJWKSBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &cappedJWKSBody{body: resp.Body, remaining: maxJWKSBodyBytes}
	return resp, nil
}

// cappedJWKSBody fails the read once the body passes the cap, which makes an
// oversized response a failed fetch: the cache keeps its previous key set and
// the error reaches the caller or the error sink like any other fetch failure.
type cappedJWKSBody struct {
	body      io.ReadCloser
	remaining int64
}

func (b *cappedJWKSBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(b.window(p))
	b.remaining -= int64(n)
	if b.remaining < 0 {
		// The fetcher abandons the body when a transform fails, so the
		// connection is only released if the overrun closes it here.
		_ = b.body.Close()
		return n, errJWKSBodyTooLarge
	}
	return n, err
}

// window trims p so one read can overshoot the cap by at most a byte; that byte
// is what tells a body exactly at the cap from one over it.
func (b *cappedJWKSBody) window(p []byte) []byte {
	if int64(len(p)) > b.remaining+1 {
		return p[:b.remaining+1]
	}
	return p
}

func (b *cappedJWKSBody) Close() error { return b.body.Close() }

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
