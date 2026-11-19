package providers

import (
	authdomain "altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/internal/core/ports"
	shared "altune/go-api/internal/shared/kernel"
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

const reasonRevoked authdomain.TokenRejectReason = "revoked"

const defaultJWKSFetchTimeout = 10 * time.Second

type jwksSchedule struct {
	now             func() time.Time
	fetchTimeout    time.Duration
	refreshInterval time.Duration
	refreshWindow   time.Duration
}

type SupabaseJWTVerifier struct {
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	audience string

	primed atomic.Bool

	refresher *jwksRefresher

	metrics ports.AuthMetrics

	revoker ports.TokenRevoker

	schedule jwksSchedule
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

func WithJWKSClock(now func() time.Time) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if now != nil {
			v.schedule.now = now
		}
	}
}

func WithJWKSFetchTimeout(timeout time.Duration) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if timeout > 0 {
			v.schedule.fetchTimeout = timeout
		}
	}
}

func WithJWKSBackgroundRefresh(interval, window time.Duration) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if interval > 0 {
			v.schedule.refreshInterval = interval
		}
		if window > 0 {
			v.schedule.refreshWindow = window
		}
	}
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string, opts ...SupabaseJWTVerifierOption) (*SupabaseJWTVerifier, error) {
	if err := requireSecureJWKSURL(jwksURL); err != nil {
		return nil, err
	}
	v := newSupabaseJWTVerifier(jwksURL, projectURL, audience, opts)
	if err := v.registerJWKS(ctx); err != nil {
		return nil, err
	}
	v.primeJWKS(ctx)
	return v, nil
}

func newSupabaseJWTVerifier(jwksURL, projectURL, audience string, opts []SupabaseJWTVerifierOption) *SupabaseJWTVerifier {
	v := &SupabaseJWTVerifier{
		jwksURL:  jwksURL,
		issuer:   strings.TrimRight(projectURL, "/") + supabaseAuthPathSuffix,
		audience: audience,
		metrics:  ports.NoopAuthMetrics(),
		revoker:  ports.NoopTokenRevoker(),
		schedule: jwksSchedule{
			now:             time.Now,
			fetchTimeout:    defaultJWKSFetchTimeout,
			refreshInterval: defaultJWKSRefreshInterval,
			refreshWindow:   defaultJWKSRefreshWindow,
		},
	}
	for _, opt := range opts {
		opt(v)
	}
	v.refresher = newJWKSRefresher(v.forceRefresh, v.schedule.now, v.schedule.fetchTimeout)
	return v
}

func (v *SupabaseJWTVerifier) registerJWKS(ctx context.Context) error {
	cache := jwk.NewCache(ctx,
		jwk.WithRefreshWindow(v.schedule.refreshWindow),
		jwk.WithErrSink(jwksErrSink(v.onBackgroundRefreshError)),
	)
	httpClient := &http.Client{
		Timeout:       v.schedule.fetchTimeout,
		CheckRedirect: checkJWKSRedirect,
		Transport:     cappedJWKSBodyTransport{base: http.DefaultTransport},
	}
	if err := cache.Register(v.jwksURL,
		jwk.WithHTTPClient(httpClient),
		jwk.WithRefreshInterval(v.schedule.refreshInterval),
		jwk.WithPostFetcher(jwk.PostFetchFunc(v.onKeySetFetched)),
	); err != nil {
		return fmt.Errorf("register JWKS URL: %w", err)
	}
	v.cache = cache
	return nil
}

func (v *SupabaseJWTVerifier) primeJWKS(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, v.schedule.fetchTimeout)
	defer cancel()
	if _, err := v.cache.Refresh(refreshCtx, v.jwksURL); err != nil {
		v.metrics.JWKSFetchFailed()
		slog.WarnContext(ctx, "initial JWKS fetch failed, will retry on first request", "error", err)
		return
	}
	v.primed.Store(true)
}

type jwksErrSink func(error)

func (f jwksErrSink) Error(err error) { f(err) }

var ErrJWKSEmptyKeySet = errors.New("JWKS response contains no keys")

func (v *SupabaseJWTVerifier) onKeySetFetched(_ string, set jwk.Set) (jwk.Set, error) {
	if set.Len() == 0 {
		return nil, ErrJWKSEmptyKeySet
	}
	v.refresher.recordKeySet()
	return set, nil
}

func (v *SupabaseJWTVerifier) onBackgroundRefreshError(err error) {
	v.metrics.JWKSFetchFailed()
	failures, age := v.refresher.recordBackgroundFailure(err)
	slog.WarnContext(context.Background(), "JWKS background refresh failed, serving last-known-good key set until it is older than stale_after, then rejecting tokens",
		"error", err,
		"consecutive_failures", failures,
		"key_set_age", age.Round(time.Second).String(),
		"stale_after", jwksStaleAfter.String(),
	)
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (authdomain.VerifiedToken, error) {
	keySet, err := v.fetchFreshKeySet(ctx)
	if err != nil {
		return authdomain.VerifiedToken{}, err
	}

	token, err := v.parse(tokenStr, keySet)
	if isSignatureRejection(err) {
		token, err = v.retryAfterKeyRefresh(ctx, tokenStr, err)
	}
	if err != nil {
		return authdomain.VerifiedToken{}, err
	}

	return v.verifyClaims(ctx, token)
}

func (v *SupabaseJWTVerifier) verifyClaims(ctx context.Context, token jwt.Token) (authdomain.VerifiedToken, error) {
	if err := checkLifetime(token); err != nil {
		return authdomain.VerifiedToken{}, err
	}

	verified, err := verifiedToken(token)
	if err != nil {
		return authdomain.VerifiedToken{}, err
	}
	if err := v.checkRevoked(ctx, verified.UserID, token.IssuedAt()); err != nil {
		return authdomain.VerifiedToken{}, err
	}
	return verified, nil
}

func (v *SupabaseJWTVerifier) checkRevoked(ctx context.Context, userID shared.UserId, issuedAt time.Time) error {
	revoked, err := v.revoker.Revoked(ctx, userID, issuedAt)
	if err != nil {
		return fmt.Errorf("check token revocation: %w", err)
	}
	if revoked {
		return &authdomain.InvalidTokenError{Reason: reasonRevoked, Detail: "token revoked"}
	}
	return nil
}

func verifiedToken(token jwt.Token) (authdomain.VerifiedToken, error) {
	if token.Expiration().IsZero() {
		return authdomain.VerifiedToken{}, &authdomain.InvalidTokenError{Reason: authdomain.ReasonClaimMissingEXP, Detail: "missing exp claim"}
	}
	userID, err := extractUserID(token)
	if err != nil {
		return authdomain.VerifiedToken{}, err
	}
	return authdomain.VerifiedToken{UserID: userID, ExpiresAt: token.Expiration()}, nil
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
		return nil, &authdomain.InvalidTokenError{Reason: classifyJWTError(err), Detail: err.Error()}
	}
	return token, nil
}

func isSignatureRejection(err error) bool {
	var tokenErr *authdomain.InvalidTokenError
	return errors.As(err, &tokenErr) && tokenErr.Reason == authdomain.ReasonSignatureInvalid
}

func (v *SupabaseJWTVerifier) retryAfterKeyRefresh(ctx context.Context, tokenStr string, rejection error) (jwt.Token, error) {
	if err := v.refresher.RefreshIfStale(ctx); err != nil {
		if errors.Is(err, errJWKSRefreshRecent) {
			return nil, rejection
		}
		return nil, fmt.Errorf("refresh JWKS after signature rejection: %w", err)
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
		return &authdomain.InvalidTokenError{
			Reason: authdomain.ReasonClaimInvalidIAT,
			Detail: "missing iat claim",
		}
	}
	if lifetime := token.Expiration().Sub(iat); lifetime > maxAccessTokenLifetime+acceptableSkew {
		return &authdomain.InvalidTokenError{
			Reason: authdomain.ReasonClaimInvalidIAT,
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
		return shared.UserId{}, &authdomain.InvalidTokenError{
			Reason: authdomain.ReasonClaimInvalidSUB,
			Detail: "missing sub claim",
		}
	}

	userId, err := shared.ParseUserId(sub)
	if err != nil {
		return shared.UserId{}, &authdomain.InvalidTokenError{
			Reason: authdomain.ReasonClaimInvalidSUB,
			Detail: "invalid sub claim: " + err.Error(),
		}
	}

	return userId, nil
}
