package auth

import (
	"altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/ports"
	"altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func Middleware(verifier ports.TokenVerifier, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := middlewareConfig{metrics: ports.NoopAuthMetrics()}
	for _, opt := range opts {
		opt(&cfg)
	}
	return middleware(verifier, newFailureThrottle(DefaultFailureLimits, time.Now), cfg.metrics)
}

type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	metrics ports.AuthMetrics
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
		a.rej.rejectToken(w, r, domain.ReasonMissing, "missing authorization header", nil)
		return "", false
	}
	token, ok := bearerToken(authHeader)
	if !ok {
		a.rej.rejectToken(w, r, domain.ReasonMalformed, "malformed authorization header", nil)
		return "", false
	}
	return token, true
}

func (a authenticator) verify(w http.ResponseWriter, r *http.Request, token string) (domain.VerifiedToken, bool) {
	attempt, retryAfter, admitted := a.throttle.admit(clientKey(r))
	if !admitted {
		a.rej.rejectThrottled(w, r, retryAfter)
		return domain.VerifiedToken{}, false
	}
	verified, err := a.verifier.Verify(r.Context(), token)
	if err != nil {
		a.rej.rejectFailedVerification(w, r, err)
		return domain.VerifiedToken{}, false
	}
	if verified.ExpiresAt.IsZero() {
		a.rej.rejectToken(w, r, domain.ReasonClaimMissingEXP, "invalid token", nil)
		return domain.VerifiedToken{}, false
	}
	attempt.succeeded()
	return verified, true
}

func contextFor(ctx context.Context, t domain.VerifiedToken) context.Context {
	return service.ContextWithTokenExpiry(service.ContextWithUserID(ctx, t.UserID), t.ExpiresAt)
}

const maxBearerTokenBytes = 8 << 10

func bearerToken(authHeader string) (string, bool) {
	scheme, token, found := strings.Cut(authHeader, " ")
	if !found || !strings.EqualFold(scheme, "bearer") || len(token) > maxBearerTokenBytes {
		return "", false
	}
	return token, true
}

type rejectResponse struct {
	Detail string `json:"detail"`
	Reason string `json:"reason"`
}

type rejecter struct {
	metrics ports.AuthMetrics
}

func (rej rejecter) rejectFailedVerification(w http.ResponseWriter, r *http.Request, err error) {
	var invalidToken *domain.InvalidTokenError
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
	httputil.WriteError(w, http.StatusServiceUnavailable, "authentication unavailable")
}

func (rej rejecter) rejectThrottled(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	rej.metrics.RequestThrottled()
	slog.WarnContext(r.Context(), "auth.throttled",
		"client", clientKey(r),
		"path", r.URL.Path,
	)
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	httputil.WriteError(w, http.StatusTooManyRequests, "too many failed authentication attempts")
}

func (rej rejecter) rejectVerifierUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	rej.metrics.VerifierUnavailable()
	slog.ErrorContext(r.Context(), "auth.verifier_unavailable",
		"error", err.Error(),
		"path", r.URL.Path,
	)
	httputil.WriteError(w, http.StatusServiceUnavailable, "authentication unavailable")
}

func (rej rejecter) rejectToken(w http.ResponseWriter, r *http.Request, reason domain.TokenRejectReason, detail string, err error) {
	rej.metrics.TokenRejected(string(reason))
	writeUnauthorized(w, r, reason, detail, err)
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, reason domain.TokenRejectReason, detail string, err error) {
	attrs := []any{
		"reason", string(reason),
		"detail", detail,
		"client", clientKey(r),
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

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := service.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, domain.ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}
