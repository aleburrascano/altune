package middleware

import (
	"altune/go-api/internal/auth/internal/core/ports"
	"altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func Middleware(verifier ports.TokenVerifier, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := middlewareConfig{metrics: ports.NoopAuthMetrics(), limits: DefaultFailureLimits, now: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}
	return middleware(verifier, newFailureThrottle(cfg.limits, cfg.now), cfg.metrics)
}

type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	metrics ports.AuthMetrics
	limits  FailureLimits
	now     func() time.Time
}

func WithFailureLimits(limits FailureLimits, now func() time.Time) MiddlewareOption {
	return func(c *middlewareConfig) {
		c.limits = limits.orDefaults()
		if now != nil {
			c.now = now
		}
	}
}

func WithMetrics(m ports.AuthMetrics) MiddlewareOption {
	return func(c *middlewareConfig) {
		if m != nil {
			c.metrics = m
		}
	}
}

func middleware(verifier ports.TokenVerifier, throttle *failureThrottle, metrics ports.AuthMetrics) func(http.Handler) http.Handler {
	a := authenticator{verifier: verifier, throttle: throttle, rej: rejecter{metrics: metrics}}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := a.bearer(w, r)
			if !ok {
				return
			}
			verified, ok := a.verify(w, r, token)
			if !ok {
				return
			}

			slog.DebugContext(r.Context(), "auth.verified",
				"user_id", verified.UserID.String(),
				logging.KeyPath, r.URL.Path,
			)

			next.ServeHTTP(w, r.WithContext(contextFor(r.Context(), verified)))
		})
	}
}

type authenticator struct {
	verifier ports.TokenVerifier
	throttle *failureThrottle
	rej      rejecter
}

func (a authenticator) bearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		a.rej.rejectToken(w, r, ports.ReasonMissing, "missing authorization header", nil)
		return "", false
	}
	token, ok := bearerToken(authHeader)
	if !ok {
		a.rej.rejectToken(w, r, ports.ReasonMalformed, "malformed authorization header", nil)
		return "", false
	}
	return token, true
}

func (a authenticator) verify(w http.ResponseWriter, r *http.Request, token string) (ports.VerifiedToken, bool) {
	attempt, retryAfter, admitted := a.throttle.admit(requestuser.ClientKey(r))
	if !admitted {
		a.rej.rejectThrottled(w, r, retryAfter)
		return ports.VerifiedToken{}, false
	}
	verified, err := a.verifier.Verify(r.Context(), token)
	if err != nil {
		a.rej.rejectFailedVerification(w, r, err)
		return ports.VerifiedToken{}, false
	}
	if verified.ExpiresAt.IsZero() {
		a.rej.rejectToken(w, r, ports.ReasonClaimMissingEXP, "invalid token", nil)
		return ports.VerifiedToken{}, false
	}
	attempt.succeeded()
	return verified, true
}

func contextFor(ctx context.Context, t ports.VerifiedToken) context.Context {
	return requestuser.ContextWithTokenExpiry(requestuser.ContextWithUserID(ctx, t.UserID), t.ExpiresAt)
}

const maxBearerTokenBytes = 8 << 10

func bearerToken(authHeader string) (string, bool) {
	scheme, token, found := strings.Cut(authHeader, " ")
	if !found || !strings.EqualFold(scheme, "bearer") || len(token) > maxBearerTokenBytes {
		return "", false
	}
	return token, true
}

var rejectionCodes = map[ports.TokenRejectReason]domain.Code{
	ports.ReasonMissing:          domain.CodeAuthTokenMissing,
	ports.ReasonMalformed:        domain.CodeAuthTokenMalformed,
	ports.ReasonSignatureInvalid: domain.CodeAuthTokenSignatureInvalid,
	ports.ReasonExpired:          domain.CodeAuthTokenExpired,
	ports.ReasonRevoked:          domain.CodeAuthTokenRevoked,
	ports.ReasonClaimMissingEXP:  domain.CodeAuthTokenClaimMissingEXP,
	ports.ReasonClaimInvalidISS:  domain.CodeAuthTokenClaimInvalidISS,
	ports.ReasonClaimInvalidAUD:  domain.CodeAuthTokenClaimInvalidAUD,
	ports.ReasonClaimInvalidSUB:  domain.CodeAuthTokenClaimInvalidSUB,
	ports.ReasonClaimInvalidIAT:  domain.CodeAuthTokenClaimInvalidIAT,
}

func rejectionCode(reason ports.TokenRejectReason) domain.Code {
	if code, ok := rejectionCodes[reason]; ok {
		return code
	}
	return domain.CodeAuthTokenInvalid
}

type rejecter struct {
	metrics ports.AuthMetrics
}

func (rej rejecter) rejectFailedVerification(w http.ResponseWriter, r *http.Request, err error) {
	var invalidToken *ports.InvalidTokenError
	if errors.As(err, &invalidToken) {
		rej.rejectToken(w, r, invalidToken.Reason, "invalid token", err)
		return
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(r.Context().Err(), context.Canceled) {
		rej.metrics.VerifierUnavailable()
	}
	httputil.HandleServiceError(w, r, fmt.Errorf("verify bearer token: %w: %w",
		domain.NewError(domain.CodeAuthUnavailable, "authentication unavailable"), err))
}

func (rej rejecter) rejectThrottled(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	rej.metrics.RequestThrottled()
	slog.WarnContext(r.Context(), "auth.throttled",
		"client", requestuser.ClientKey(r),
		logging.KeyPath, r.URL.Path,
	)
	httputil.HandleServiceError(w, r, domain.NewError(domain.CodeAuthThrottled, "too many failed authentication attempts").WithRetryAfter(retryAfter))
}

func (rej rejecter) rejectToken(w http.ResponseWriter, r *http.Request, reason ports.TokenRejectReason, detail string, err error) {
	rej.metrics.TokenRejected(string(reason))
	attrs := []any{
		"reason", string(reason),
		"detail", detail,
		"client", requestuser.ClientKey(r),
		logging.KeyPath, r.URL.Path,
	}
	if err != nil {
		attrs = append(attrs, logging.KeyError, err.Error())
	}
	slog.WarnContext(r.Context(), "auth.token_rejected", attrs...)
	w.Header().Set("WWW-Authenticate", "Bearer")
	httputil.HandleServiceError(w, r, domain.NewError(rejectionCode(reason), detail))
}

var (
	RequireUserID = requestuser.RequireUserID
	ClientKey     = requestuser.ClientKey
)
