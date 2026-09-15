package providers

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const acceptableSkew = 5 * time.Second

// maxAccessTokenLifetime is the longest exp-iat window Verify accepts.
//
// Revocation is deliberately out of scope: Verify is purely stateless (JWKS
// signature, iss, aud, exp, iat) and never consults a denylist or re-queries
// account status, so a token issued before a ban, deletion, forced logout, or
// password/role change keeps authenticating until it expires. That risk is
// accepted only because the window is short, and this constant enforces it here
// rather than trusting the Supabase project's JWT-expiry setting: a token whose
// lifetime exceeds one hour (Supabase's default access-token TTL, and the
// project's live setting: real tokens carry exp-iat = 3600) is rejected, so
// raising the project setting fails loudly (401 claim_invalid_iat) instead of
// silently widening the revocation gap; lower it freely.
// Renewal is where Supabase enforces revocation: it refuses refresh-token grants
// for signed-out sessions and banned or deleted users, so a revoked session
// cannot obtain a fresh access token once this window closes. A denylist fed by
// Supabase auth events was considered and waived in #1032 as disproportionate
// to a bounded one-hour exposure.
const maxAccessTokenLifetime = time.Hour

// jwksFetchTimeout bounds every HTTP call to the JWKS endpoint: both the
// startup fetch and the httprc refresh-worker's request use it, so a slow or
// hung endpoint can neither block startup nor permanently exhaust the shared
// fetch-worker pool. It is a var only so tests can shorten it; production code
// never reassigns it.
var jwksFetchTimeout = 10 * time.Second

type SupabaseJWTVerifier struct {
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	audience string

	// primed flips to true once a JWKS fetch has succeeded. Until then,
	// fetchKeySet forces a refresh on every call: the httprc cache marks the URL
	// as "already fetched" after the first attempt regardless of outcome, so a
	// plain Get would otherwise wait for the ~15-minute background refresh window
	// rather than retrying on the next request as documented.
	primed atomic.Bool
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string) (*SupabaseJWTVerifier, error) {
	if err := requireSecureJWKSURL(jwksURL); err != nil {
		return nil, err
	}
	cache := jwk.NewCache(ctx)

	// Give the cache's fetch worker an HTTP client with a bounded timeout. The
	// worker performs the actual HTTP call with a non-context client, so only
	// the client's own Timeout can stop one stuck fetch from blocking a worker
	// forever (the pool has just 3 workers shared across all callers).
	// CheckRedirect keeps a redirect from downgrading the fetch to plaintext.
	httpClient := &http.Client{Timeout: jwksFetchTimeout, CheckRedirect: checkJWKSRedirect}
	if err := cache.Register(jwksURL, jwk.WithHTTPClient(httpClient)); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}

	issuer := strings.TrimRight(projectURL, "/") + "/auth/v1"

	v := &SupabaseJWTVerifier{
		cache:    cache,
		jwksURL:  jwksURL,
		issuer:   issuer,
		audience: audience,
	}

	// Bound the startup fetch so app.Run cannot hang forever on a hung endpoint.
	refreshCtx, cancel := context.WithTimeout(ctx, jwksFetchTimeout)
	defer cancel()
	if _, err := cache.Refresh(refreshCtx, jwksURL); err != nil {
		slog.Warn("initial JWKS fetch failed, will retry on first request", "error", err)
	} else {
		v.primed.Store(true)
	}

	return v, nil
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (shared.UserId, error) {
	keySet, err := v.fetchKeySet(ctx)
	if err != nil {
		return shared.UserId{}, err
	}

	token, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithAcceptableSkew(acceptableSkew),
	)
	if err != nil {
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: classifyJWTError(err),
			Detail: err.Error(),
		}
	}

	if err := checkLifetime(token); err != nil {
		return shared.UserId{}, err
	}

	return extractUserID(token)
}

// checkLifetime enforces maxAccessTokenLifetime, bounding how long a revoked
// session's already-issued token can keep authenticating. A missing iat is
// rejected because without it the lifetime cannot be bounded.
func checkLifetime(token jwt.Token) error {
	iat := token.IssuedAt()
	if iat.IsZero() {
		return &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidIAT,
			Detail: "missing iat claim",
		}
	}
	if lifetime := token.Expiration().Sub(iat); lifetime > maxAccessTokenLifetime+acceptableSkew {
		return &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidIAT,
			Detail: fmt.Sprintf("token lifetime %s exceeds maximum %s", lifetime, maxAccessTokenLifetime),
		}
	}
	return nil
}

// fetchKeySet retrieves the JWKS key set from the refresh-worker-backed cache.
// Extracted from Verify's former inline v.cache.Get call so key-set retrieval is
// a single-purpose step, separate from structural validation and claim mapping.
func (v *SupabaseJWTVerifier) fetchKeySet(ctx context.Context) (jwk.Set, error) {
	// When no JWKS fetch has ever succeeded, force a refresh so a transient
	// startup failure recovers on this request instead of waiting for the
	// background refresh window. Refresh uses the registered bounded HTTP
	// client, so a hung endpoint still cannot block past jwksFetchTimeout.
	if !v.primed.Load() {
		if _, err := v.cache.Refresh(ctx, v.jwksURL); err != nil {
			return nil, fmt.Errorf("fetch JWKS: %w", err)
		}
		v.primed.Store(true)
	}

	keySet, err := v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	return keySet, nil
}

// CheckHealth reports whether the auth subsystem can obtain its JWKS key set. It
// runs through the same path incoming requests use, so a typo'd or unreachable
// JWKS URL surfaces as a degraded dependency, and a transient startup failure is
// re-attempted here too. Returns nil when the key set is available.
func (v *SupabaseJWTVerifier) CheckHealth(ctx context.Context) error {
	_, err := v.fetchKeySet(ctx)
	return err
}

// extractUserID maps a validated token's claims onto a shared.UserId. Extracted
// from Verify's former inline exp/sub presence checks and ParseUserId call so
// claim mapping changes for its own reason, independent of key retrieval.
func extractUserID(token jwt.Token) (shared.UserId, error) {
	if _, ok := token.Get(jwt.ExpirationKey); !ok {
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimMissingEXP,
			Detail: "missing exp claim",
		}
	}

	sub := token.Subject()
	if sub == "" {
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidSUB,
			Detail: "missing sub claim",
		}
	}

	userId, err := shared.ParseUserId(sub)
	if err != nil {
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidSUB,
			Detail: "invalid sub claim: " + err.Error(),
		}
	}

	return userId, nil
}
