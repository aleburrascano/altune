package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// maxJWKSBytes bounds the JWKS response Overseer reads, so a hostile or runaway
// endpoint cannot exhaust memory. A real JWKS is a few KiB.
const maxJWKSBytes = 1 << 20 // 1 MiB

// jwksHTTPTimeout bounds one JWKS fetch end to end.
const jwksHTTPTimeout = 10 * time.Second

// minRefetchInterval throttles JWKS refetches triggered by an unknown kid, so a
// stream of tokens bearing forged/unknown kids cannot turn the guard into a
// request amplifier against Supabase.
const minRefetchInterval = 30 * time.Second

// jwksCache fetches and caches Supabase signing keys by kid. It refetches at most
// once per minRefetchInterval when asked for a kid it does not hold (key
// rotation), and never grows without bound: it holds only the current key set.
type jwksCache struct {
	url  string
	http *http.Client

	mu          sync.Mutex
	keys        map[string]any
	lastFetched time.Time
}

func newJWKSCache(url string, httpClient *http.Client) *jwksCache {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: jwksHTTPTimeout, CheckRedirect: refuseRedirect}
	}
	return &jwksCache{url: url, http: httpClient, keys: make(map[string]any)}
}

// key returns the public key for kid, fetching (or refetching, throttled) the
// JWKS when the kid is not cached. It returns ErrNoKey when the key cannot be
// found after a fetch, so an asymmetric token with an unknown kid fails closed.
func (c *jwksCache) key(ctx context.Context, kid string) (any, error) {
	if k := c.cached(kid); k != nil {
		return k, nil
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	if k := c.cached(kid); k != nil {
		return k, nil
	}
	return nil, ErrNoKey
}

func (c *jwksCache) cached(kid string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[kid]
}

// refresh fetches the JWKS and replaces the cached key set. It is throttled: a
// call within minRefetchInterval of the last fetch is a no-op, so unknown-kid
// spam cannot amplify into unbounded upstream fetches.
func (c *jwksCache) refresh(ctx context.Context) error {
	c.mu.Lock()
	if !c.lastFetched.IsZero() && time.Since(c.lastFetched) < minRefetchInterval {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	keys, err := c.fetch(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.keys = keys
	c.lastFetched = time.Now()
	c.mu.Unlock()
	return nil
}

// fetch downloads and parses the JWKS into a kid→public-key map.
func (c *jwksCache) fetch(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("authn: build jwks request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authn: fetch jwks: %w", err)
	}
	if resp == nil {
		return nil, errors.New("authn: nil jwks response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authn: jwks endpoint returned status %d", resp.StatusCode)
	}
	var doc jwksDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("authn: decode jwks: %w", err)
	}
	out := make(map[string]any, len(doc.Keys))
	for _, jwk := range doc.Keys {
		pub, err := jwk.publicKey()
		if err != nil || jwk.Kid == "" {
			continue // skip a key we cannot use rather than failing the whole set
		}
		out[jwk.Kid] = pub
	}
	return out, nil
}

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

// jwk is one JSON Web Key. Only the fields needed to reconstruct an RSA or EC
// public key are read.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// publicKey reconstructs the crypto public key from the JWK, supporting the RSA
// and EC key types Supabase issues.
func (k jwk) publicKey() (any, error) {
	switch k.Kty {
	case "RSA":
		return k.rsaKey()
	case "EC":
		return k.ecKey()
	default:
		return nil, fmt.Errorf("authn: unsupported jwk kty %q", k.Kty)
	}
}

func (k jwk) rsaKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, errors.New("authn: bad rsa modulus")
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, errors.New("authn: bad rsa exponent")
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		// Some encoders pad; fall back to a 4-byte big-endian read.
		padded := make([]byte, 4)
		copy(padded[4-len(eBytes):], eBytes)
		e = int(binary.BigEndian.Uint32(padded))
	}
	if e == 0 {
		return nil, errors.New("authn: zero rsa exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

func (k jwk) ecKey() (*ecdsa.PublicKey, error) {
	curve, err := ecCurve(k.Crv)
	if err != nil {
		return nil, err
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, errors.New("authn: bad ec x")
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, errors.New("authn: bad ec y")
	}
	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

func ecCurve(crv string) (elliptic.Curve, error) {
	switch crv {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("authn: unsupported ec curve %q", crv)
	}
}

// refuseRedirect forbids the JWKS client from following any redirect: the JWKS
// endpoint is a fixed same-project URL, and following a redirect could send the
// request (and any future credential) somewhere unintended.
func refuseRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}
