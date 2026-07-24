package adapters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const acceptableSkew = 5 * time.Second

type SupabaseJWTVerifier struct {
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	audience string
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string) (*SupabaseJWTVerifier, error) {
	cache := jwk.NewCache(ctx)

	if err := cache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}

	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		slog.Warn("initial JWKS fetch failed, will retry on first request", "error", err)
	}

	issuer := projectURL + "/auth/v1"

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
