package adapters

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type SupabaseJWTVerifier struct {
	cache      *jwk.Cache
	jwksURL    string
	issuer     string
	audience   string
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string) (*SupabaseJWTVerifier, error) {
	cache := jwk.NewCache(ctx)

	if err := cache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}

	// Force initial fetch to fail fast on bad URL
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
		return shared.UserId{}, &auth.InvalidTokenError{Reason: "failed to fetch JWKS"}
	}

	token, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return shared.UserId{}, &auth.InvalidTokenError{Reason: err.Error()}
	}

	sub := token.Subject()
	if sub == "" {
		return shared.UserId{}, &auth.InvalidTokenError{Reason: "missing sub claim"}
	}

	userId, err := shared.ParseUserId(sub)
	if err != nil {
		return shared.UserId{}, &auth.InvalidTokenError{Reason: "invalid sub claim: " + err.Error()}
	}

	return userId, nil
}
