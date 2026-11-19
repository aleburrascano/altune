package middleware

import (
	"altune/go-api/internal/auth/internal/core/ports"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
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
				"path", r.URL.Path,
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

const (
	codeAuthThrottled   = "auth.throttled"
	codeAuthUnavailable = "auth.unavailable"
)

type rejectResponse struct {
	Detail string `json:"detail"`
	Reason string `json:"reason"`
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
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		rej.rejectClientCancelled(w, r, err)
		return
	}
	rej.rejectVerifierUnavailable(w, r, err)
}

func (rej rejecter) rejectClientCancelled(w http.ResponseWriter, r *http.Request, err error) {
	slog.InfoContext(r.Context(), "auth.client_cancelled",
		"error", err.Error(),
		"path", r.URL.Path,
	)
	writeAuthUnavailable(w)
}

func (rej rejecter) rejectThrottled(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	rej.metrics.RequestThrottled()
	slog.WarnContext(r.Context(), "auth.throttled",
		"client", requestuser.ClientKey(r),
		"path", r.URL.Path,
	)
	w.Header().Set("Retry-After", httputil.RetryAfterSeconds(retryAfter))
	httputil.WriteJSON(w, http.StatusTooManyRequests, httputil.ErrorResponse{
		Detail: "too many failed authentication attempts",
		Code:   codeAuthThrottled,
	})
}

func writeAuthUnavailable(w http.ResponseWriter) {
	httputil.WriteJSON(w, http.StatusServiceUnavailable, httputil.ErrorResponse{
		Detail: "authentication unavailable",
		Code:   codeAuthUnavailable,
	})
}

func (rej rejecter) rejectVerifierUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	rej.metrics.VerifierUnavailable()
	slog.ErrorContext(r.Context(), "auth.verifier_unavailable",
		"error", err.Error(),
		"path", r.URL.Path,
	)
	writeAuthUnavailable(w)
}

func (rej rejecter) rejectToken(w http.ResponseWriter, r *http.Request, reason ports.TokenRejectReason, detail string, err error) {
	rej.metrics.TokenRejected(string(reason))
	writeUnauthorized(w, r, reason, detail, err)
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, reason ports.TokenRejectReason, detail string, err error) {
	attrs := []any{
		"reason", string(reason),
		"detail", detail,
		"client", requestuser.ClientKey(r),
		"path", r.URL.Path,
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.WarnContext(r.Context(), "auth.token_rejected", attrs...)

	w.Header().Set("WWW-Authenticate", "Bearer")
	httputil.WriteJSON(w, http.StatusUnauthorized, rejectResponse{
		Detail: detail,
		Reason: string(reason),
	})
}

var (
	RequireUserID = requestuser.RequireUserID
	ClientKey     = requestuser.ClientKey
)
