package providers

import (
	"altune/go-api/internal/platform/auth/internal/core/ports"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const acceptableSkew = 5 * time.Second

const supabaseAuthPathSuffix = "/auth/v1"

const maxAccessTokenLifetime = time.Hour

const defaultJWKSFetchTimeout = 10 * time.Second

type jwksSchedule struct {
	now             clock.Func
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

type JWKSSchedule struct {
	FetchTimeout    time.Duration
	RefreshInterval time.Duration
	RefreshWindow   time.Duration
}

func DefaultJWKSSchedule() JWKSSchedule {
	return JWKSSchedule{
		FetchTimeout:    defaultJWKSFetchTimeout,
		RefreshInterval: defaultJWKSRefreshInterval,
		RefreshWindow:   defaultJWKSRefreshWindow,
	}
}

func (s JWKSSchedule) orDefaults() JWKSSchedule {
	defaults := DefaultJWKSSchedule()
	if s.FetchTimeout <= 0 {
		s.FetchTimeout = defaults.FetchTimeout
	}
	if s.RefreshInterval <= 0 {
		s.RefreshInterval = defaults.RefreshInterval
	}
	if s.RefreshWindow <= 0 {
		s.RefreshWindow = defaults.RefreshWindow
	}
	return s
}

var (
	ErrJWKSClockRequired  = errors.New("providers: the JWT verifier requires a clock")
	ErrJWKSJitterRequired = errors.New("providers: the JWT verifier requires a jitter source")
)

func NewSupabaseJWTVerifier(
	ctx context.Context,
	jwksURL, projectURL, audience string,
	now clock.Func,
	jitter random.Unit,
	metrics ports.AuthMetrics,
	revoker ports.TokenRevoker,
	schedule JWKSSchedule,
) (*SupabaseJWTVerifier, error) {
	if now == nil {
		return nil, ErrJWKSClockRequired
	}
	if jitter == nil {
		return nil, fmt.Errorf("NewSupabaseJWTVerifier: %w", ErrJWKSJitterRequired)
	}
	if metrics == nil || revoker == nil {
		panic("providers: NewSupabaseJWTVerifier requires metrics and a token revoker")
	}
	if err := requireSecureJWKSURL(jwksURL); err != nil {
		return nil, err
	}
	v := newSupabaseJWTVerifier(jwksURL, projectURL, audience, now, jitter, metrics, revoker, schedule.orDefaults())
	if err := v.registerJWKS(ctx); err != nil {
		return nil, err
	}
	v.primeJWKS(ctx)
	return v, nil
}

func newSupabaseJWTVerifier(
	jwksURL, projectURL, audience string,
	now clock.Func,
	jitter random.Unit,
	metrics ports.AuthMetrics,
	revoker ports.TokenRevoker,
	schedule JWKSSchedule,
) *SupabaseJWTVerifier {
	v := &SupabaseJWTVerifier{
		jwksURL:  jwksURL,
		issuer:   strings.TrimRight(projectURL, "/") + supabaseAuthPathSuffix,
		audience: audience,
		metrics:  metrics,
		revoker:  revoker,
		schedule: jwksSchedule{
			now:             now,
			fetchTimeout:    schedule.FetchTimeout,
			refreshInterval: schedule.RefreshInterval,
			refreshWindow:   schedule.RefreshWindow,
		},
	}
	v.refresher = newJWKSRefresher(v.forceRefresh, v.schedule.now, jitter, v.schedule.fetchTimeout)
	return v
}

func (v *SupabaseJWTVerifier) registerJWKS(ctx context.Context) error {
	cache := jwk.NewCache(ctx,
		jwk.WithRefreshWindow(v.schedule.refreshWindow),
		jwk.WithErrSink(jwksErrSink(func(err error) { v.onBackgroundRefreshError(ctx, err) })),
	)
	httpClient, err := outbound.NewClient(v.schedule.fetchTimeout, nil, outbound.WithBodyCap(maxJWKSBodyBytes))
	if err != nil {
		return fmt.Errorf("build JWKS client: %w", err)
	}
	httpClient.CheckRedirect = checkJWKSRedirect
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

func (v *SupabaseJWTVerifier) onBackgroundRefreshError(ctx context.Context, err error) {
	v.metrics.JWKSFetchFailed()
	failures, age := v.refresher.recordBackgroundFailure(err)
	slog.WarnContext(ctx, "JWKS background refresh failed, serving last-known-good key set until it is older than stale_after, then rejecting tokens",
		"error", err,
		"consecutive_failures", failures,
		"key_set_age", age.Round(time.Second).String(),
		"stale_after", jwksStaleAfter.String(),
	)
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (ports.VerifiedToken, error) {
	keySet, err := v.fetchFreshKeySet(ctx)
	if err != nil {
		return ports.VerifiedToken{}, err
	}

	token, err := v.parse(tokenStr, keySet)
	if isSignatureRejection(err) {
		token, err = v.retryAfterKeyRefresh(ctx, tokenStr, err)
	}
	if err != nil {
		return ports.VerifiedToken{}, err
	}

	return v.verifyClaims(ctx, token)
}

func (v *SupabaseJWTVerifier) verifyClaims(ctx context.Context, token jwt.Token) (ports.VerifiedToken, error) {
	if err := checkLifetime(token); err != nil {
		return ports.VerifiedToken{}, err
	}

	verified, err := verifiedToken(token)
	if err != nil {
		return ports.VerifiedToken{}, err
	}
	if err := v.checkRevoked(ctx, verified.UserID, token.IssuedAt()); err != nil {
		return ports.VerifiedToken{}, err
	}
	return verified, nil
}

func (v *SupabaseJWTVerifier) checkRevoked(ctx context.Context, userID ids.UserId, issuedAt time.Time) error {
	revoked, err := v.revoker.Revoked(ctx, userID, issuedAt)
	if err != nil {
		return fmt.Errorf("check token revocation: %w", err)
	}
	if revoked {
		return &ports.InvalidTokenError{Reason: ports.ReasonRevoked, Detail: "token revoked"}
	}
	return nil
}

func verifiedToken(token jwt.Token) (ports.VerifiedToken, error) {
	if token.Expiration().IsZero() {
		return ports.VerifiedToken{}, &ports.InvalidTokenError{Reason: ports.ReasonClaimMissingEXP, Detail: "missing exp claim"}
	}
	userID, err := extractUserID(token)
	if err != nil {
		return ports.VerifiedToken{}, err
	}
	return ports.VerifiedToken{UserID: userID, ExpiresAt: token.Expiration()}, nil
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
		return nil, &ports.InvalidTokenError{Reason: classifyJWTError(err), Detail: err.Error()}
	}
	return token, nil
}

func isSignatureRejection(err error) bool {
	var tokenErr *ports.InvalidTokenError
	return errors.As(err, &tokenErr) && tokenErr.Reason == ports.ReasonSignatureInvalid
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
		return &ports.InvalidTokenError{
			Reason: ports.ReasonClaimInvalidIAT,
			Detail: "missing iat claim",
		}
	}
	if lifetime := token.Expiration().Sub(iat); lifetime > maxAccessTokenLifetime+acceptableSkew {
		return &ports.InvalidTokenError{
			Reason: ports.ReasonClaimInvalidIAT,
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
	keySet, err = v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("read the JWKS after a refresh: %w", err)
	}
	return keySet, nil
}

func (v *SupabaseJWTVerifier) forceRefresh(ctx context.Context) error {
	if _, err := v.cache.Refresh(ctx, v.jwksURL); err != nil {
		v.metrics.JWKSFetchFailed()
		return fmt.Errorf("refresh the JWKS: %w", err)
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

func extractUserID(token jwt.Token) (ids.UserId, error) {
	sub := token.Subject()
	if sub == "" {
		return ids.UserId{}, &ports.InvalidTokenError{
			Reason: ports.ReasonClaimInvalidSUB,
			Detail: "missing sub claim",
		}
	}

	userId, err := ids.ParseUserId(sub)
	if err != nil {
		return ids.UserId{}, &ports.InvalidTokenError{
			Reason: ports.ReasonClaimInvalidSUB,
			Detail: "invalid sub claim: " + err.Error(),
		}
	}

	return userId, nil
}
