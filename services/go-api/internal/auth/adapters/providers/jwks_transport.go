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

var errJWKSBodyTooLarge = fmt.Errorf("JWKS response body exceeds %d bytes", maxJWKSBodyBytes)

type cappedJWKSBodyTransport struct{ base http.RoundTripper }

func (t cappedJWKSBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &cappedJWKSBody{body: resp.Body, remaining: maxJWKSBodyBytes}
	return resp, nil
}

type cappedJWKSBody struct {
	body      io.ReadCloser
	remaining int64
}

func (b *cappedJWKSBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(b.window(p))
	b.remaining -= int64(n)
	if b.remaining < 0 {
		_ = b.body.Close()
		return n, errJWKSBodyTooLarge
	}
	return n, err
}

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
