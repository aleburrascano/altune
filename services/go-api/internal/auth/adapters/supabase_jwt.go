package adapters

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const acceptableSkew = 5 * time.Second

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
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string) (*SupabaseJWTVerifier, error) {
	cache := jwk.NewCache(ctx)

	// Give the cache's fetch worker an HTTP client with a bounded timeout. The
	// worker performs the actual HTTP call with a non-context client, so only
	// the client's own Timeout can stop one stuck fetch from blocking a worker
	// forever (the pool has just 3 workers shared across all callers).
	httpClient := &http.Client{Timeout: jwksFetchTimeout}
	if err := cache.Register(jwksURL, jwk.WithHTTPClient(httpClient)); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}

	// Bound the startup fetch so app.Run cannot hang forever on a hung endpoint.
	refreshCtx, cancel := context.WithTimeout(ctx, jwksFetchTimeout)
	defer cancel()
	if _, err := cache.Refresh(refreshCtx, jwksURL); err != nil {
		slog.Warn("initial JWKS fetch failed, will retry on first request", "error", err)
	}

	issuer := strings.TrimRight(projectURL, "/") + "/auth/v1"

	return &SupabaseJWTVerifier{
		cache:    cache,
		jwksURL:  jwksURL,
		issuer:   issuer,
		audience: audience,
	}, nil
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (shared.UserId, error) {
	keySet, err := v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return shared.UserId{}, fmt.Errorf("fetch JWKS: %w", err)
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
		reason := classifyJWTError(err)
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: reason,
			Detail: err.Error(),
		}
	}

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

func classifyJWTError(err error) auth.TokenRejectReason {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired()):
		return auth.ReasonExpired
	case errors.Is(err, jwt.ErrInvalidIssuer()):
		return auth.ReasonClaimInvalidISS
	case errors.Is(err, jwt.ErrInvalidAudience()):
		return auth.ReasonClaimInvalidAUD
	case hasUntypedSignatureFailure(err):
		return auth.ReasonSignatureInvalid
	default:
		return auth.ReasonMalformed
	}
}

func hasUntypedSignatureFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "failed to find key") ||
		strings.Contains(msg, "could not verify message")
}
