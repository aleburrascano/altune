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

// supabaseAuthPathSuffix is the path Supabase appends to a project URL to form
// the token issuer (the iss claim), e.g. https://<ref>.supabase.co/auth/v1.
const supabaseAuthPathSuffix = "/auth/v1"

// maxAccessTokenLifetime is the longest exp-iat window Verify accepts.
//
// Revocation is deliberately out of scope: Verify is purely stateless (JWKS
// signature, iss, aud, exp, iat) and never consults a denylist or re-queries
// account status, so a token issued before a ban, deletion, forced logout, or
// password/role change keeps authenticating until it expires. That risk is
// accepted only because the window is short, and this constant enforces it here
// rather than trusting the Supabase project's JWT-expiry setting: a token whose
// lifetime exceeds one hour (Supabase's default access-token TTL, and the
// project's live setting: real tokens carry exp-iat = 3600) is rejected, so
// raising the project setting fails loudly (401 claim_invalid_iat) instead of
// silently widening the revocation gap; lower it freely.
// Renewal is where Supabase enforces revocation: it refuses refresh-token grants
// for signed-out sessions and banned or deleted users, so a revoked session
// cannot obtain a fresh access token once this window closes. A denylist fed by
// Supabase auth events was considered and waived in #1032 as disproportionate
// to a bounded one-hour exposure.
const maxAccessTokenLifetime = time.Hour

// jwksFetchTimeout bounds every HTTP call to the JWKS endpoint: both the
// startup fetch and the httprc refresh-worker's request use it, so a slow or
// hung endpoint can neither block startup nor permanently exhaust the shared
// fetch-worker pool. It is a var only so tests can shorten it; production code
// never reassigns it.
var jwksFetchTimeout = 10 * time.Second

type SupabaseJWTVerifier struct {
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	audience string

	// primed flips to true once a JWKS fetch has succeeded. Until then,
	// fetchKeySet forces a refresh: the httprc cache marks the URL as "already
	// fetched" after the first attempt regardless of outcome, so a plain Get
	// would otherwise wait for the ~15-minute background refresh window rather
	// than retrying on the next request as documented.
	primed atomic.Bool

	// refresher coalesces those forced refreshes, and the ones an unknown
	// signing key triggers after a rotation, into one in-flight fetch; it backs
	// off after failures and rate-limits unknown-key refreshes after a success,
	// so neither a cold-start outage nor a flood of made-up kids costs one fetch
	// per request.
	refresher *jwksRefresher

	// metrics counts every failed JWKS fetch (startup, forced, background), so
	// a JWKS outage is one number rather than only log lines.
	metrics ports.AuthMetrics
}

// SupabaseJWTVerifierOption configures NewSupabaseJWTVerifier.
type SupabaseJWTVerifierOption func(*SupabaseJWTVerifier)

// WithJWKSMetrics makes the verifier count failed JWKS fetches through m. A nil
// m keeps the no-op default.
func WithJWKSMetrics(m ports.AuthMetrics) SupabaseJWTVerifierOption {
	return func(v *SupabaseJWTVerifier) {
		if m != nil {
			v.metrics = m
		}
	}
}

func NewSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string, opts ...SupabaseJWTVerifierOption) (*SupabaseJWTVerifier, error) {
	return newSupabaseJWTVerifier(ctx, jwksURL, projectURL, audience, time.Now, opts...)
}

// newSupabaseJWTVerifier is NewSupabaseJWTVerifier with an injectable clock for
// the refresher's backoff and staleness bookkeeping. The clock is fixed before
// the cache's background worker starts, so tests can drive it without racing.
func newSupabaseJWTVerifier(ctx context.Context, jwksURL, projectURL, audience string, now func() time.Time, opts ...SupabaseJWTVerifierOption) (*SupabaseJWTVerifier, error) {
	if err := requireSecureJWKSURL(jwksURL); err != nil {
		return nil, err
	}

	v := &SupabaseJWTVerifier{
		jwksURL:  jwksURL,
		issuer:   strings.TrimRight(projectURL, "/") + supabaseAuthPathSuffix,
		audience: audience,
		metrics:  ports.NoopAuthMetrics(),
	}
	for _, opt := range opts {
		opt(v)
	}
	v.refresher = newJWKSRefresher(v.forceRefresh)
	v.refresher.now = now

	// The error sink receives every failed background refresh, which the cache
	// otherwise drops while it keeps serving the last good key set.
	cache := jwk.NewCache(ctx,
		jwk.WithRefreshWindow(jwksRefreshWindow),
		jwk.WithErrSink(jwksErrSink(v.onBackgroundRefreshError)),
	)

	// Give the cache's fetch worker an HTTP client with a bounded timeout. The
	// worker performs the actual HTTP call with a non-context client, so only
	// the client's own Timeout can stop one stuck fetch from blocking a worker
	// forever (the pool has just 3 workers shared across all callers).
	// CheckRedirect keeps a redirect from downgrading the fetch to plaintext,
	// and the transport caps the response body the worker will buffer.
	// The post-fetcher runs after every fetch that parses, so it is the one
	// place that decides whether startup, forced, and background refreshes
	// alike delivered a usable key set.
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

	// Bound the startup fetch so app.Run cannot hang forever on a hung endpoint.
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

// jwksErrSink adapts a function to the cache's error sink interface.
type jwksErrSink func(error)

func (f jwksErrSink) Error(err error) { f(err) }

// errJWKSEmptyKeySet marks a JWKS response that parsed but published no keys.
// It verifies nothing, so storing it would reject every token until the next
// successful refresh; the cache must keep the last good set instead.
var errJWKSEmptyKeySet = errors.New("JWKS response contains no keys")

// onKeySetFetched is the cache's post-fetch hook: it records that a usable key
// set was just stored and passes the set through unchanged. Rejecting an empty
// set here makes it a failed fetch on every path at once (startup, forced,
// background): the cache keeps the previous set, the error reaches the caller
// or the error sink to be logged and counted, and the staleness clock only
// advances on a set that can actually verify a token.
func (v *SupabaseJWTVerifier) onKeySetFetched(_ string, set jwk.Set) (jwk.Set, error) {
	if set.Len() == 0 {
		return nil, errJWKSEmptyKeySet
	}
	v.refresher.recordKeySet()
	return set, nil
}

// onBackgroundRefreshError logs a failed background refresh. The cache keeps
// serving the previous key set, so this line (and the staleness CheckHealth
// derives from the same bookkeeping) is the only trace of the fallback firing.
func (v *SupabaseJWTVerifier) onBackgroundRefreshError(err error) {
	v.metrics.JWKSFetchFailed()
	failures, age := v.refresher.recordBackgroundFailure(err)
	slog.Warn("JWKS background refresh failed, serving last-known-good key set",
		"error", err,
		"consecutive_failures", failures,
		"key_set_age", age.Round(time.Second).String(),
		"stale_after", jwksStaleAfter.String(),
	)
}

func (v *SupabaseJWTVerifier) Verify(ctx context.Context, tokenStr string) (shared.UserId, error) {
	verified, err := v.VerifyExpiring(ctx, tokenStr)
	return verified.UserID, err
}

func (v *SupabaseJWTVerifier) VerifyExpiring(ctx context.Context, tokenStr string) (auth.VerifiedToken, error) {
	keySet, err := v.fetchKeySet(ctx)
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

	return verifiedToken(token)
}

func verifiedToken(token jwt.Token) (auth.VerifiedToken, error) {
	userID, err := extractUserID(token)
	if err != nil {
		return auth.VerifiedToken{}, err
	}
	return auth.VerifiedToken{UserID: userID, ExpiresAt: token.Expiration()}, nil
}

// parse verifies tokenStr's signature against keySet and validates its standard
// claims, mapping any failure onto an *auth.InvalidTokenError.
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

// isSignatureRejection reports whether err is a signature failure (unknown kid
// or failed verification), the only rejection a newer key set could overturn.
func isSignatureRejection(err error) bool {
	var tokenErr *auth.InvalidTokenError
	return errors.As(err, &tokenErr) && tokenErr.Reason == auth.ReasonSignatureInvalid
}

// retryAfterKeyRefresh handles a signing-key rotation: the token may be signed
// with a key published after the cached set was fetched, and the cache would
// otherwise serve the stale set until its background refresh (15+ minutes). It
// forces one rate-limited refresh and parses once more. If no refresh happens
// (fetched recently, backing off, or failed), the original rejection stands, so
// the outcome is still a 401 and never a JWKS-unavailable error.
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

// checkLifetime enforces maxAccessTokenLifetime, bounding how long a revoked
// session's already-issued token can keep authenticating. A missing iat is
// rejected because without it the lifetime cannot be bounded.
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

// fetchKeySet retrieves the JWKS key set from the refresh-worker-backed cache.
// Extracted from Verify's former inline v.cache.Get call so key-set retrieval is
// a single-purpose step, separate from structural validation and claim mapping.
func (v *SupabaseJWTVerifier) fetchKeySet(ctx context.Context) (jwk.Set, error) {
	// When no JWKS fetch has ever succeeded, force a refresh so a transient
	// startup failure recovers on this request instead of waiting for the
	// background refresh window. The refresher shares one in-flight fetch among
	// concurrent callers, fails fast while backing off after a failure, and
	// releases each caller when its own ctx ends.
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

// forceRefresh performs one real JWKS fetch through the cache and marks the
// verifier primed on success. Only the refresher calls it, never concurrently.
func (v *SupabaseJWTVerifier) forceRefresh(ctx context.Context) error {
	if _, err := v.cache.Refresh(ctx, v.jwksURL); err != nil {
		v.metrics.JWKSFetchFailed()
		return err
	}
	v.primed.Store(true)
	return nil
}

// CheckHealth reports whether the auth subsystem can obtain a current JWKS key
// set. It runs through the same path incoming requests use, so a typo'd or
// unreachable JWKS URL, or one publishing no keys, surfaces as a degraded
// dependency, and a transient startup failure is re-attempted here too. Once
// primed, the cache keeps serving its last good key set however long background
// refreshes fail, so CheckHealth also reports errJWKSStale when that set is
// older than jwksStaleAfter. It never forces a refresh for staleness:
// background refreshes already retry, and a failed one leaves the age growing
// until one succeeds. Returns nil when a fresh-enough key set is available.
func (v *SupabaseJWTVerifier) CheckHealth(ctx context.Context) error {
	if _, err := v.fetchKeySet(ctx); err != nil {
		return err
	}
	return v.refresher.checkFresh()
}

// extractUserID maps a validated token's claims onto a shared.UserId. Extracted
// from Verify's former inline exp/sub presence checks and ParseUserId call so
// claim mapping changes for its own reason, independent of key retrieval.
func extractUserID(token jwt.Token) (shared.UserId, error) {
	if _, ok := token.Get(jwt.ExpirationKey); !ok {
		return shared.UserId{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimMissingEXP,
			Detail: "missing exp claim",
		}
	}

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
