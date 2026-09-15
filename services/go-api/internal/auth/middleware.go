package auth

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
)

type contextKey struct{}

var userIDKey contextKey

// Middleware authenticates the bearer token on every request. Each client
// address may fail verification only DefaultFailureLimits times before further
// attempts are refused with 429 without running the verifier, so an
// unauthenticated caller cannot drive unbounded verification or JWKS work.
func Middleware(verifier TokenVerifier) func(http.Handler) http.Handler {
	return middleware(verifier, newFailureThrottle(DefaultFailureLimits, time.Now))
}

func middleware(verifier TokenVerifier, throttle *failureThrottle) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				rejectToken(w, r, ReasonMissing, "missing authorization header", nil)
				return
			}

			token, ok := bearerToken(authHeader)
			if !ok {
				rejectToken(w, r, ReasonMalformed, "malformed authorization header", nil)
				return
			}

			attempt, retryAfter, admitted := throttle.admit(clientKey(r))
			if !admitted {
				rejectThrottled(w, r, retryAfter)
				return
			}

			userId, err := verifier.Verify(r.Context(), token)
			if err != nil {
				rejectFailedVerification(w, r, err)
				return
			}
			attempt.succeeded()

			slog.DebugContext(r.Context(), "auth.verified",
				"user_id", userId.String(),
				"path", r.URL.Path,
			)

			next.ServeHTTP(w, r.WithContext(ContextWithUserID(r.Context(), userId)))
		})
	}
}

func bearerToken(authHeader string) (string, bool) {
	scheme, token, found := strings.Cut(authHeader, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return "", false
	}
	return token, true
}

func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

type rejectResponse struct {
	Detail string `json:"detail"`
	Reason string `json:"reason"`
}

func rejectFailedVerification(w http.ResponseWriter, r *http.Request, err error) {
	var invalidToken *InvalidTokenError
	if errors.As(err, &invalidToken) {
		rejectToken(w, r, invalidToken.Reason, "invalid token", err)
		return
	}
	rejectVerifierUnavailable(w, r, err)
}

// rejectThrottled refuses before verification runs, so the response carries no
// token reject reason and is identical whatever bearer value was sent.
func rejectThrottled(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	slog.WarnContext(r.Context(), "auth.throttled",
		"client", clientKey(r),
		"path", r.URL.Path,
	)
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	httputil.WriteError(w, http.StatusTooManyRequests, "too many failed authentication attempts")
}

func rejectVerifierUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "auth.verifier_unavailable",
		"error", err.Error(),
		"path", r.URL.Path,
	)
	httputil.WriteError(w, http.StatusServiceUnavailable, "authentication unavailable")
}

func rejectToken(w http.ResponseWriter, r *http.Request, reason TokenRejectReason, detail string, err error) {
	attrs := []any{"reason", string(reason), "detail", detail}
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

func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	id, ok := ctx.Value(userIDKey).(shared.UserId)
	return id, ok
}

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		rejectToken(w, r, ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}
