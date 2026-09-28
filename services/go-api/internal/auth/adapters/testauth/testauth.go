package testauth

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

var testUserUUID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func TestUserId() shared.UserId { return shared.NewUserId(testUserUUID) }

const (
	tokenIssuer   = "altune-test-auth"
	tokenAudience = "altune-test"

	tokenLifetime = time.Hour

	signingAlg = jwa.HS256

	acceptableSkew = 5 * time.Second

	signingKeyBytes = 32
)

type TestAuth struct {
	key []byte
	now func() time.Time
}

func New() (*TestAuth, error) {
	key := make([]byte, signingKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate test-auth signing key: %w", err)
	}
	return &TestAuth{key: key, now: time.Now}, nil
}

func (t *TestAuth) IssueToken() (string, time.Time, error) {
	now := t.now()
	exp := now.Add(tokenLifetime)
	tok, err := jwt.NewBuilder().
		Issuer(tokenIssuer).
		Audience([]string{tokenAudience}).
		Subject(testUserUUID.String()).
		IssuedAt(now).
		Expiration(exp).
		Build()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build test token: %w", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(signingAlg, t.key))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign test token: %w", err)
	}
	return string(signed), exp, nil
}

func (t *TestAuth) Verify(_ context.Context, tokenStr string) (auth.VerifiedToken, error) {
	tok, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKey(signingAlg, t.key),
		jwt.WithValidate(true),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithAcceptableSkew(acceptableSkew),
		jwt.WithClock(jwt.ClockFunc(t.now)),
	)
	if err != nil {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid, Detail: err.Error()}
	}
	if tok.Subject() != testUserUUID.String() {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidSUB,
			Detail: "test token subject is not the dedicated test user",
		}
	}
	return auth.VerifiedToken{UserID: TestUserId(), ExpiresAt: tok.Expiration()}, nil
}

func Combine(test, fallback auth.TokenVerifier) auth.TokenVerifier {
	return auth.VerifierFunc(func(ctx context.Context, token string) (auth.VerifiedToken, error) {
		if verified, err := test.Verify(ctx, token); err == nil {
			return verified, nil
		}
		return fallback.Verify(ctx, token)
	})
}
