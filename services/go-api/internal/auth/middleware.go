package auth

import (
	"altune/go-api/internal/auth/ports"
	"altune/go-api/internal/shared/httputil"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Middleware authenticates the bearer token on every request. Each client
// address may fail verification only DefaultFailureLimits times before further
// attempts are refused with 429 without running the verifier, so an
// unauthenticated caller cannot drive unbounded verification or JWKS work.
//
// Every 401, 429 and 503 it writes is also counted through the metrics given by
// WithMetrics (no-op by default), so a rejection, lockout or outage spike is one
// number.
func Middleware(verifier TokenVerifier, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := middlewareConfig{metrics: ports.NoopAuthMetrics()}
	for _, opt := range opts {
		opt(&cfg)
	}
	return middleware(verifier, newFailureThrottle(DefaultFailureLimits, time.Now), cfg.metrics)
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	metrics ports.AuthMetrics
}

// WithMetrics makes Middleware count token rejections, throttled requests and
// verifier unavailability through m. A nil m keeps the no-op default.
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

			verified, err := VerifyToken(r.Context(), verifier, token)
			if err != nil {
				rej.rejectFailedVerification(w, r, err)
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

// maxBearerTokenBytes is this module's own ceiling on a bearer value, so an
// oversized token never reaches jwt.Parse whatever the server's MaxHeaderBytes.
// A real Supabase ES256 access token measured 807 bytes (2026-09, password
// sign-in); OAuth identity metadata or custom claims can grow it by a few
// hundred bytes to a couple of KB, so 8 KiB leaves ~10x headroom.
//
// An oversized bearer is rejected as malformed before failure-throttle
// admission, like any other unparseable header: the check is constant-time and
// does no verification work, so it spends none of the caller's failure budget.
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

// rejecter writes the 401/429/503 responses and counts each one.
type rejecter struct {
	metrics ports.AuthMetrics
}

func (rej rejecter) rejectFailedVerification(w http.ResponseWriter, r *http.Request, err error) {
	var invalidToken *InvalidTokenError
	if errors.As(err, &invalidToken) {
		rej.rejectToken(w, r, invalidToken.Reason, "invalid token", err)
		return
	}
	rej.rejectVerifierUnavailable(w, r, err)
}

// rejectThrottled refuses before verification runs, so the response carries no
// token reject reason and is identical whatever bearer value was sent.
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
	rejectToken(w, r, reason, detail, err)
}

// rejectToken logs and writes a 401 without counting it. Only the middleware's
// rejecter counts: RequireUserID reuses this for a handler reached without the
// middleware, which is a wiring bug rather than a client's rejected token.
func rejectToken(w http.ResponseWriter, r *http.Request, reason TokenRejectReason, detail string, err error) {
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
