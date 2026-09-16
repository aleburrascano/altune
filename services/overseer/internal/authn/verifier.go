// Package authn verifies Supabase JWTs for Overseer's owner-only guard. It
// verifies the signature locally — asymmetric tokens against the project's JWKS
// (fetched and cached, no per-request Supabase round-trip), and legacy HS256
// tokens against an optionally-configured project secret — enforces expiry, and
// returns the token's subject for the allowlist check. It never trusts an
// unsigned (alg=none) token and never verifies an asymmetric token with the
// symmetric secret, closing the two classic JWT algorithm-confusion holes.
package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	jwt "github.com/golang-jwt/jwt/v5"
)

// Sentinel verification failures. None carries token material, so they are safe
// to wrap and log. The shell maps any of them to 401 (a non-owner subject, by
// contrast, is a 403 the shell decides after a successful Verify).
var (
	// ErrNoKey means no key was available to verify the token's algorithm — an
	// HS256 token with no configured secret, or an asymmetric token whose kid is
	// not in the JWKS. Fail closed.
	ErrNoKey = errors.New("authn: no verification key for token")
	// ErrUnsupportedAlg means the token's alg is not one of the accepted methods
	// (notably alg=none is never accepted).
	ErrUnsupportedAlg = errors.New("authn: unsupported token algorithm")
	// ErrNoSubject means the verified token carried no subject claim, so there is
	// no identity to check against the owner allowlist.
	ErrNoSubject = errors.New("authn: token has no subject claim")
)

// Claims is the minimal, verified claim set the guard needs: the subject the
// owner allowlist is checked against.
type Claims struct {
	Subject string
}

// Verifier verifies Supabase JWTs. It is safe for concurrent use.
type Verifier struct {
	jwks       *jwksCache
	hsSecret   []byte
	validAlgs  []string
	parserOpts []jwt.ParserOption
}

// New builds a Verifier. jwksURL is the Supabase JWKS endpoint used for
// asymmetric (RS*/ES*) tokens; hsSecret, when non-empty, additionally accepts
// legacy HS256 tokens. httpClient fetches the JWKS (a timeout-bounded default is
// used when nil). At least one of a reachable JWKS or an HS secret must yield a
// key at verify time, or every token fails closed.
func New(jwksURL, hsSecret string, httpClient *http.Client) *Verifier {
	// Accept the asymmetric algorithms Supabase issues, plus HS256 only when a
	// secret is configured. A static allowlist means alg=none and any unexpected
	// algorithm are rejected before a key is ever consulted.
	algs := []string{
		jwt.SigningMethodES256.Alg(),
		jwt.SigningMethodRS256.Alg(),
	}
	if hsSecret != "" {
		algs = append(algs, jwt.SigningMethodHS256.Alg())
	}
	v := &Verifier{
		jwks:      newJWKSCache(jwksURL, httpClient),
		hsSecret:  []byte(hsSecret),
		validAlgs: algs,
	}
	v.parserOpts = []jwt.ParserOption{
		jwt.WithValidMethods(algs),
		jwt.WithExpirationRequired(),
	}
	return v
}

// Verify checks the token's signature and expiry and returns its subject. A
// verification failure (bad signature, expired, unsupported/absent alg, unknown
// key, malformed) is returned as an error; the caller treats any error as
// unauthenticated (401) and only then compares the subject to the owner id.
func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims, v.keyfunc(ctx), v.parserOpts...)
	if err != nil {
		return Claims{}, fmt.Errorf("authn: verify: %w", err)
	}
	if claims.Subject == "" {
		return Claims{}, ErrNoSubject
	}
	return Claims{Subject: claims.Subject}, nil
}

// keyfunc returns the key golang-jwt uses to verify the parsed token, selected by
// the token's signing method so a symmetric token can never be verified with an
// asymmetric key or vice versa. The ctx is captured so a JWKS refresh on an
// unknown kid is bounded by the request.
func (v *Verifier) keyfunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		switch t.Method.(type) {
		case *jwt.SigningMethodHMAC:
			if len(v.hsSecret) == 0 {
				return nil, ErrNoKey
			}
			return v.hsSecret, nil
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
			kid, _ := t.Header["kid"].(string)
			return v.jwks.key(ctx, kid)
		default:
			return nil, ErrUnsupportedAlg
		}
	}
}
