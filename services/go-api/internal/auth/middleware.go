package auth

import (
	"altune/go-api/internal/auth/ports"
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

func Middleware(verifier TokenVerifier, opts ...MiddlewareOption) func(http.Handler) http.Handler {
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

func middleware(verifier TokenVerifier, throttle *failureThrottle, metrics ports.AuthMetrics) func(http.Handler) http.Handler {
	rej := rejecter{metrics: metrics}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				rej.rejectToken(w, r, ReasonMissing, "missing authorization header", nil)
				return
			}

			token, ok := bearerToken(authHeader)
			if !ok {
				rej.rejectToken(w, r, ReasonMalformed, "malformed authorization header", nil)
				return
			}

			attempt, retryAfter, admitted := throttle.admit(clientKey(r))
			if !admitted {
				rej.rejectThrottled(w, r, retryAfter)
				return
			}

			verified, err := verifier.Verify(r.Context(), token)
			if err != nil {
				rej.rejectFailedVerification(w, r, err)
				return
			}
			if verified.ExpiresAt.IsZero() {
				rej.rejectToken(w, r, ReasonClaimMissingEXP, "invalid token", nil)
				return
			}
			attempt.succeeded()

			slog.DebugContext(r.Context(), "auth.verified",
				"user_id", verified.UserID.String(),
				"path", r.URL.Path,
			)

			next.ServeHTTP(w, r.WithContext(verified.contextFor(r.Context())))
		})
	}
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
	var invalidToken *InvalidTokenError
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

func (rej rejecter) rejectToken(w http.ResponseWriter, r *http.Request, reason TokenRejectReason, detail string, err error) {
	rej.metrics.TokenRejected(string(reason))
	writeUnauthorized(w, r, reason, detail, err)
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, reason TokenRejectReason, detail string, err error) {
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
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}
