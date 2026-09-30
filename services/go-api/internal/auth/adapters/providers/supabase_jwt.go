package providers

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const acceptableSkew = 5 * time.Second

const supabaseAuthPathSuffix = "/auth/v1"

const maxAccessTokenLifetime = time.Hour

const reasonRevoked auth.TokenRejectReason = "revoked"

var jwksFetchTimeout = 10 * time.Second

type SupabaseJWTVerifier struct {
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	audience string

	primed atomic.Bool

	refresher *jwksRefresher

	metrics ports.AuthMetrics

	revoker ports.TokenRevoker
}

type SupabaseJWTVerifierOption func(*SupabaseJWTVerifier)

func WithJWKSMetrics(m ports.AuthMetrics) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if m != nil {
			v.metrics = m
		}
	}
}

func WithTokenRevoker(r ports.TokenRevoker) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if r != nil {
			v.revoker = r
		}
	}
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string, opts ...SupabaseJWTVerifierOption) (*SupabaseJWTVerifier, error) {
	return newSupabaseJWTVerifier(ctx, jwksURL, projectURL, audience, time.Now, opts...)
}

func newSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string, now func() time.Time, opts ...SupabaseJWTVerifierOption) (*SupabaseJWTVerifier, error) {
	if err := requireSecureJWKSURL(jwksURL); err != nil {
		return nil, err
	}

	v := &SupabaseJWTVerifier{
		jwksURL:  jwksURL,
		issuer:   strings.TrimRight(projectURL, "/") + supabaseAuthPathSuffix,
		audience: audience,
		metrics:  ports.NoopAuthMetrics(),
		revoker:  ports.NoopTokenRevoker(),
	}
	for _, opt := range opts {
		opt(v)
	}
	v.refresher = newJWKSRefresher(v.forceRefresh)
	v.refresher.now = now

	cache := jwk.NewCache(ctx,
		jwk.WithRefreshWindow(jwksRefreshWindow),
		jwk.WithErrSink(jwksErrSink(v.onBackgroundRefreshError)),
	)

	httpClient := &http.Client{
		Timeout:       jwksFetchTimeout,
		CheckRedirect: checkJWKSRedirect,
		Transport:     cappedJWKSBodyTransport{base: http.DefaultTransport},
	}
	if err := cache.Register(jwksURL,
		jwk.WithHTTPClient(httpClient),
		jwk.WithRefreshInterval(jwksBackgroundRefreshInterval),
		jwk.WithPostFetcher(jwk.PostFetchFunc(v.onKeySetFetched)),
	); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}
	v.cache = cache

	refreshCtx, cancel := context.WithTimeout(ctx, jwksFetchTimeout)
	defer cancel()
	if _, err := cache.Refresh(refreshCtx, jwksURL); err != nil {
		v.metrics.JWKSFetchFailed()
		slog.Warn("initial JWKS fetch failed, will retry on first request", "error", err)
	} else {
		v.primed.Store(true)
	}

	return v, nil
}

type jwksErrSink func(error)

func (f jwksErrSink) Error(err error) { f(err) }

var errJWKSEmptyKeySet = errors.New("JWKS response contains no keys")

func (v *SupabaseJWTVerifier) onKeySetFetched(_ string, set jwk.Set) (jwk.Set, error) {
	if set.Len() == 0 {
		return nil, errJWKSEmptyKeySet
	}
	v.refresher.recordKeySet()
	return set, nil
}

func (v *SupabaseJWTVerifier) onBackgroundRefreshError(err error) {
	v.metrics.JWKSFetchFailed()
	failures, age := v.refresher.recordBackgroundFailure(err)
	slog.Warn("JWKS background refresh failed, serving last-known-good key set until it is older than stale_after, then rejecting tokens",
		"error", err,
		"consecutive_failures", failures,
		"key_set_age", age.Round(time.Second).String(),
		"stale_after", jwksStaleAfter.String(),
	)
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (auth.VerifiedToken, error) {
	keySet, err := v.fetchFreshKeySet(ctx)
	if err != nil {
		return auth.VerifiedToken{}, err
	}

	token, err := v.parse(tokenStr, keySet)
	if isSignatureRejection(err) {
		token, err = v.retryAfterKeyRefresh(ctx, tokenStr, err)
	}
	if err != nil {
		return auth.VerifiedToken{}, err
	}

	if err := checkLifetime(token); err != nil {
		return auth.VerifiedToken{}, err
	}

	verified, err := verifiedToken(token)
	if err != nil {
		return auth.VerifiedToken{}, err
	}
	if err := v.checkRevoked(ctx, verified.UserID, token.IssuedAt()); err != nil {
		return auth.VerifiedToken{}, err
	}
	return verified, nil
}

func (v *SupabaseJWTVerifier) checkRevoked(ctx context.Context, userID shared.UserId, issuedAt time.Time) error {
	revoked, err := v.revoker.Revoked(ctx, userID, issuedAt)
	if err != nil {
		return fmt.Errorf("check token revocation: %w", err)
	}
	if revoked {
		return &auth.InvalidTokenError{Reason: reasonRevoked, Detail: "token revoked"}
	}
	return nil
}

func verifiedToken(token jwt.Token) (auth.VerifiedToken, error) {
	if token.Expiration().IsZero() {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonClaimMissingEXP, Detail: "missing exp claim"}
	}
	userID, err := extractUserID(token)
	if err != nil {
		return auth.VerifiedToken{}, err
	}
	return auth.VerifiedToken{UserID: userID, ExpiresAt: token.Expiration()}, nil
}

func (v *SupabaseJWTVerifier) parse(tokenStr string, keySet jwk.Set) (jwt.Token, error) {
	token, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithAcceptableSkew(acceptableSkew),
	)
	if err != nil {
		return nil, &auth.InvalidTokenError{Reason: classifyJWTError(err), Detail: err.Error()}
	}
	return token, nil
}

func isSignatureRejection(err error) bool {
	var tokenErr *auth.InvalidTokenError
	return errors.As(err, &tokenErr) && tokenErr.Reason == auth.ReasonSignatureInvalid
}

func (v *SupabaseJWTVerifier) retryAfterKeyRefresh(ctx context.Context, tokenStr string, rejection error) (jwt.Token, error) {
	if err := v.refresher.RefreshIfStale(ctx); err != nil {
		return nil, rejection
	}
	keySet, err := v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, rejection
	}
	return v.parse(tokenStr, keySet)
}

func checkLifetime(token jwt.Token) error {
	iat := token.IssuedAt()
	if iat.IsZero() {
		return &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidIAT,
			Detail: "missing iat claim",
		}
	}
	if lifetime := token.Expiration().Sub(iat); lifetime > maxAccessTokenLifetime+acceptableSkew {
		return &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidIAT,
			Detail: fmt.Sprintf("token lifetime %s exceeds maximum %s", lifetime, maxAccessTokenLifetime),
		}
	}
	return nil
}

func (v *SupabaseJWTVerifier) fetchKeySet(ctx context.Context) (jwk.Set, error) {
	if !v.primed.Load() {
		if err := v.refresher.Refresh(ctx); err != nil {
			return nil, fmt.Errorf("fetch JWKS: %w", err)
		}
	}

	keySet, err := v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	return keySet, nil
}

func (v *SupabaseJWTVerifier) fetchFreshKeySet(ctx context.Context) (jwk.Set, error) {
	keySet, err := v.fetchKeySet(ctx)
	if err != nil {
		return nil, err
	}
	staleErr := v.refresher.checkFresh()
	if staleErr == nil {
		return keySet, nil
	}
	if err := v.refresher.RefreshIfStale(ctx); err != nil {
		return nil, staleErr
	}
	if err := v.refresher.checkFresh(); err != nil {
		return nil, err
	}
	return v.cache.Get(ctx, v.jwksURL)
}

func (v *SupabaseJWTVerifier) forceRefresh(ctx context.Context) error {
	if _, err := v.cache.Refresh(ctx, v.jwksURL); err != nil {
		v.metrics.JWKSFetchFailed()
		return err
	}
	v.primed.Store(true)
	return nil
}

func (v *SupabaseJWTVerifier) CheckHealth(ctx context.Context) error {
	if _, err := v.fetchKeySet(ctx); err != nil {
		return err
	}
	return v.refresher.checkFresh()
}

func extractUserID(token jwt.Token) (shared.UserId, error) {
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
