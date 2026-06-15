package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

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
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: auth.ReasonSignatureInvalid,
			Detail: "failed to fetch JWKS",
		}
	}

	token, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
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
	msg := err.Error()
	switch {
	case strings.Contains(msg, "exp") && strings.Contains(msg, "not satisfied"):
		return auth.ReasonExpired
	case strings.Contains(msg, "iss") && strings.Contains(msg, "not satisfied"):
		return auth.ReasonClaimInvalidISS
	case strings.Contains(msg, "aud") && strings.Contains(msg, "not satisfied"):
		return auth.ReasonClaimInvalidAUD
	case strings.Contains(msg, "failed to find key"):
		return auth.ReasonSignatureInvalid
	case strings.Contains(msg, "could not verify message"):
		return auth.ReasonSignatureInvalid
	default:
		return auth.ReasonMalformed
	}
}
