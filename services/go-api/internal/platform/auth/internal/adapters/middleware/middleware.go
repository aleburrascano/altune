package middleware

import (
	"altune/go-api/internal/platform/auth/internal/core/ports"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
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

var ErrClockRequired = errors.New("middleware: the auth middleware requires a clock")

func Middleware(verifier ports.TokenVerifier, now clock.Func, metrics ports.AuthMetrics, limits FailureLimits) (func(http.Handler) http.Handler, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	if verifier == nil || metrics == nil {
		panic("middleware: the auth middleware requires a verifier and metrics")
	}
	return middleware(verifier, newFailureThrottle(limits.orDefaults(), now), metrics), nil
}

func MustMiddleware(verifier ports.TokenVerifier, now clock.Func, metrics ports.AuthMetrics, limits FailureLimits) func(http.Handler) http.Handler {
	mw, err := Middleware(verifier, now, metrics, limits)
	if err != nil {
		panic(err)
	}
	return mw
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

			httpserver.RecordRequestUser(r.Context(), verified.UserID.String())
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

var rejectionCodes = map[ports.TokenRejectReason]apierror.Code{
	ports.ReasonMissing:          apierror.CodeAuthTokenMissing,
	ports.ReasonMalformed:        apierror.CodeAuthTokenMalformed,
	ports.ReasonSignatureInvalid: apierror.CodeAuthTokenSignatureInvalid,
	ports.ReasonExpired:          apierror.CodeAuthTokenExpired,
	ports.ReasonRevoked:          apierror.CodeAuthTokenRevoked,
	ports.ReasonClaimMissingEXP:  apierror.CodeAuthTokenClaimMissingEXP,
	ports.ReasonClaimInvalidISS:  apierror.CodeAuthTokenClaimInvalidISS,
	ports.ReasonClaimInvalidAUD:  apierror.CodeAuthTokenClaimInvalidAUD,
	ports.ReasonClaimInvalidSUB:  apierror.CodeAuthTokenClaimInvalidSUB,
	ports.ReasonClaimInvalidIAT:  apierror.CodeAuthTokenClaimInvalidIAT,
}

func rejectionCode(reason ports.TokenRejectReason) apierror.Code {
	if code, ok := rejectionCodes[reason]; ok {
		return code
	}
	return apierror.CodeAuthTokenInvalid
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
	rej.rejectVerifierUnavailable(w, r, err)
}

func (rej rejecter) rejectVerifierUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, context.Canceled) || !errors.Is(r.Context().Err(), context.Canceled) {
		rej.metrics.VerifierUnavailable()
	}
	httpserver.HandleServiceError(w, r, fmt.Errorf("verify bearer token: %w: %w",
		apierror.NewError(apierror.CodeAuthUnavailable, "authentication unavailable"), err))
}

func (rej rejecter) rejectThrottled(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	rej.metrics.RequestThrottled()
	slog.WarnContext(r.Context(), "auth.throttled",
		"client", requestuser.ClientKey(r),
		logging.KeyPath, r.URL.Path,
	)
	httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeAuthThrottled, "too many failed authentication attempts").WithRetryAfter(retryAfter))
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
	httpserver.HandleServiceError(w, r, apierror.NewError(rejectionCode(reason), detail))
}

var (
	RequireUserID = requestuser.RequireUserID
	ClientKey     = requestuser.ClientKey
)
