package goapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// refreshGrantPath is the Supabase (GoTrue) token-exchange path. The grant type
// is a query parameter, so the full endpoint is
// {OVERSEER_SUPABASE_URL}/auth/v1/token?grant_type=refresh_token.
const refreshGrantPath = "/auth/v1/token"

// maxTokenResponseBytes bounds how much of the token endpoint's response the
// client reads. A hostile or runaway Supabase cannot exhaust Overseer's memory
// through an unbounded refresh body; a real token response is a few KiB.
const maxTokenResponseBytes = 1 << 16 // 64 KiB

// refreshHTTPTimeout bounds one token exchange end to end. The exchange runs on a
// shared goroutine that every waiting bucket blocks on, so a hung Supabase must
// terminate here rather than wedge the whole fleet's next refresh.
const refreshHTTPTimeout = 10 * time.Second

const (
	storeLockWait  = 3 * refreshHTTPTimeout
	storeLockRetry = 50 * time.Millisecond
)

// refreshLeadNum/refreshLeadDen place the proactive-refresh point at 4/5 (80%) of
// the access token's lifetime, in integer math so there is no float drift.
const (
	refreshLeadNum = 4
	refreshLeadDen = 5
)

// maxAcceptedLifetime caps the access-token lifetime the proactive-refresh math
// trusts. A token whose exp is absurdly far in the future — a hostile or
// misconfigured Supabase — would otherwise both overflow the 4/5 multiply below
// and pin the cache for years; capping bounds the refresh cadence so the source
// stays live and the arithmetic can never overflow. go-api independently rejects
// any token whose own lifetime exceeds one hour, so a real token is well inside
// this bound.
const maxAcceptedLifetime = 24 * time.Hour

// minRefreshInterval floors the proactive window so a pathologically short-lived
// token cannot spin the refresh goroutine hot.
const minRefreshInterval = 5 * time.Second

// refreshBackoffBase/refreshBackoffMax bound how fast a failing exchange is
// re-attempted. A spent seed refresh token (Supabase 400 refresh_token_already_used)
// otherwise draws one exchange per collect cycle, and Supabase turns that stream
// into a 429 that then fights the operator's reseed. Capped exponential backoff
// throttles a persistently-failing endpoint to a few probes an hour while a brief
// transient (a 5xx blip) still recovers on the next attempt — and because the cap
// is finite, backoff never wedges: a healed endpoint, or a reseed after restart, is
// picked up within one cap interval. Deliberately no failure classification: every
// failure backs off the same way, so a transient 5xx can never be mistaken for a
// terminal 400 and permanently wedged.
const (
	refreshBackoffBase = 1 * time.Second
	refreshBackoffMax  = 5 * time.Minute
)

// Selection env vars. Every one of them names the READ-ONLY principal: go-api
// refuses that subject on its mutating admin routes, so the credential Overseer
// holds cannot change production even if the host is compromised. When all three
// refresh vars are present the source refreshes; otherwise it falls back to the
// static read-only token, then to a fail-closed null source.
const (
	envSupabaseURL          = "OVERSEER_SUPABASE_URL"
	envSupabaseAnon         = "OVERSEER_SUPABASE_ANON_KEY"
	envReadOnlyRefreshToken = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN"
	envReadOnlyRefreshFile  = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE"
	envReadOnlyToken        = "OVERSEER_GOAPI_READONLY_TOKEN"
	logRefreshSource        = "goapi: token source selection"
)

// The operator credentials Overseer used before #1810. They are read only to be
// refused: an operator token carries write scope, so falling back to one would
// quietly restore the capability the read-only principal exists to drop.
const (
	envLegacyOperatorRefreshToken = "OVERSEER_GOAPI_REFRESH_TOKEN"
	envLegacyOperatorToken        = "OVERSEER_GOAPI_TOKEN"
)

// defaultRefreshTokenPath is where the rotating read-only refresh token is
// persisted when OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE is unset.
// compose.prod.yml mounts a named volume here, so the live refresh chain
// survives a container restart instead of falling back to the already-spent seed
// in the environment. The file name is the principal's, not "refresh_token":
// that older path holds the operator chain on deployed volumes, and seeding from
// it would hand this source an operator token (seedFromStore prefers the
// persisted value over the env seed).
const defaultRefreshTokenPath = "/var/lib/overseer/readonly_refresh_token"

// Sentinel causes for a failed exchange. None carries token material, so they are
// safe to wrap into a *TokenRefreshError and log.
var (
	errMalformedAccessToken = errors.New("malformed access token")
	errMissingExpClaim      = errors.New("access token missing exp claim")
	errExpiredAccessToken   = errors.New("access token already expired")
	errMissingAccessToken   = errors.New("token response missing access_token")
	errRefreshRejected      = errors.New("token endpoint rejected the refresh grant")
	errStoreLockUnavailable = errors.New("refresh token file lock not acquired")
)

// TokenRefreshError is the typed failure a RefreshingTokenSource returns when it
// cannot exchange the refresh token for an access token. It is the signal buckets
// degrade on: the client fails closed (no unauthenticated request leaves) and the
// bucket renders its last-known state stale rather than panicking. It carries NO
// token material — neither the refresh token nor any access token ever appears in
// its message — only the stage that failed and, for a non-2xx, the status code.
type TokenRefreshError struct {
	// Stage names where the exchange failed (build, transport, status, decode,
	// claims), for diagnostics.
	Stage string
	// Status is the token endpoint's HTTP status when the failure was a non-2xx
	// response, or 0 otherwise.
	Status int
	// Err is the underlying cause, guaranteed free of token material.
	Err error
}

func (e *TokenRefreshError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("goapi: read-only token refresh failed at %s: status %d", e.Stage, e.Status)
	}
	return fmt.Sprintf("goapi: read-only token refresh failed at %s: %v", e.Stage, e.Err)
}

// Unwrap exposes the cause to errors.Is/As.
func (e *TokenRefreshError) Unwrap() error { return e.Err }

// RefreshingTokenSource keeps a read-only access token live indefinitely by
// exchanging a long-lived Supabase refresh token for fresh access tokens. It
// caches the access token, refreshes proactively at ~80% of its lifetime, and
// coalesces concurrent refreshes into ONE exchange (single-flight) so many
// buckets sharing one source trigger a single network call — which also means the
// rotating refresh token is never used by two exchanges at once. It satisfies
// TokenSource, so it drops into the client and both SSE consumers unchanged.
type RefreshingTokenSource struct {
	endpoint string
	anonKey  string
	http     *http.Client
	now      func() time.Time

	// store, when non-nil, persists the rotating refresh token across restarts:
	// it seeds the source at construction (read-on-start) and records each
	// rotation (write-on-rotate). Nil keeps the default env-only behavior.
	store refreshTokenStore

	// mu guards the cached token material, its refresh deadline, the single-flight
	// slot and the backoff state. Every read and write of a secret goes through it.
	mu          sync.Mutex
	accessToken string
	refreshTok  string
	refreshAt   time.Time // proactive-refresh deadline; a cached token is served until it
	inflight    *refreshCall

	// Backoff after a failed exchange. Every refresh failure — spent token,
	// transient 5xx, rate limit — pushes retryAt out on backoff's capped
	// exponential curve so a persistently-failing endpoint is probed a few times an
	// hour, not once per collect cycle (the storm that turned a 400 into a 429 in
	// prod). failCount is the consecutive-failure count that drives the curve; a
	// successful exchange resets both, so recovery is prompt. lastErr is the typed
	// failure surfaced to buckets while backed off, so they degrade to source-down
	// without touching the network; it is non-nil exactly while retryAt is in the
	// future.
	backoff   Backoff
	failCount int
	retryAt   time.Time
	lastErr   error

	storedTok     string
	persistFailed bool
}

// refreshCall is one in-flight exchange shared by every caller that joined it.
// Its result fields are written once, before done is closed, and read only after
// the receive — the channel close is the happens-before, so no lock guards them.
type refreshCall struct {
	done  chan struct{}
	token string
	err   error
}

// RefreshingOption customizes a RefreshingTokenSource at construction.
type RefreshingOption func(*RefreshingTokenSource)

// WithRefreshHTTPClient supplies the *http.Client used for token exchanges (tests
// or a shared transport). A nil client is ignored, keeping the timeout-bounded
// default.
func WithRefreshHTTPClient(h *http.Client) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if h != nil {
			s.http = h
		}
	}
}

// withClock injects the clock the proactive-refresh math reads. Unexported: only
// the package's own tests drive expiry with a fake clock; production uses
// time.Now.
func withClock(now func() time.Time) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if now != nil {
			s.now = now
		}
	}
}

// WithRefreshTokenStore makes the source persist the rotating refresh token across
// restarts. At construction it loads any previously-persisted token (resuming the
// live chain) and thereafter writes each rotation back. A nil store — the default —
// keeps the env-only behavior the unit tests rely on. Because Supabase spends the
// seed on first use, without a store a restart replays that spent seed and every
// go-api-backed bucket degrades to source-down.
func WithRefreshTokenStore(store refreshTokenStore) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if store != nil {
			s.store = store
		}
	}
}

// refreshTokenStore is the pluggable persistence seam: read-on-start, write-on-
// rotate. Implementations MUST never log the token value and MUST leave any backing
// file non-world-readable. load returns "" (not an error) when nothing is stored
// yet, so the caller falls back to the env seed on first boot.
type refreshTokenStore interface {
	lock(ctx context.Context) (release func(), err error)
	load() (string, error)
	save(token string) error
}

// fileRefreshTokenStore persists the rotating refresh token to a single file on a
// mounted volume. The token value never reaches a log — only the path (a non-secret)
// and failures are surfaced. Writes are atomic (temp file + rename) so a crash mid-
// rotation cannot leave a truncated token that bricks the next boot.
type fileRefreshTokenStore struct {
	path string
}

func (s fileRefreshTokenStore) lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, err
	}
	fileLock := flock.New(s.path + ".lock")
	locked, err := fileLock.TryLockContext(ctx, storeLockRetry)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errStoreLockUnavailable
	}
	return func() { _ = fileLock.Unlock() }, nil
}

// load returns the persisted refresh token, or "" when the file is absent (first
// boot) or holds only whitespace. Any other read failure surfaces so a mounted-but-
// unreadable volume fails startup loudly rather than silently replaying the seed.
func (s fileRefreshTokenStore) load() (string, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// save durably records the rotated refresh token at chmod 0600 via a temp file and
// rename, so the persisted token is never world-readable and never half-written.
func (s fileRefreshTokenStore) save(token string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".refresh_token-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if err := writeTokenFile(tmp, token); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// writeTokenFile chmods to 0600, writes the token and closes, so save reads as one
// step and the file handle is released on every path.
func writeTokenFile(f *os.File, token string) error {
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.WriteString(token); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// NewRefreshingTokenSource builds a refreshing read-only TokenSource that exchanges
// refreshToken at {supabaseURL}/auth/v1/token?grant_type=refresh_token, presenting
// anonKey as the apikey header. It errors on a blank or unparseable supabaseURL, a
// blank anonKey or a blank refreshToken, so misconfiguration fails at startup
// rather than at first refresh.
func NewRefreshingTokenSource(supabaseURL, anonKey, refreshToken string, opts ...RefreshingOption) (*RefreshingTokenSource, error) {
	base, err := parseBaseURL(supabaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(anonKey) == "" {
		return nil, errors.New("goapi: empty Supabase anon key")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("goapi: empty Supabase refresh token")
	}

	endpoint := base.JoinPath(refreshGrantPath)
	q := endpoint.Query()
	q.Set("grant_type", "refresh_token")
	endpoint.RawQuery = q.Encode()

	s := &RefreshingTokenSource{
		endpoint:   endpoint.String(),
		anonKey:    anonKey,
		refreshTok: refreshToken,
		http:       &http.Client{Timeout: refreshHTTPTimeout, CheckRedirect: refuseRedirect},
		now:        time.Now,
		backoff:    NewExpBackoff(refreshBackoffBase, refreshBackoffMax),
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.seedFromStore(); err != nil {
		return nil, err
	}
	return s, nil
}

// seedFromStore replaces the env seed with a previously-persisted rotated token so
// a restart resumes the live chain. A blank persisted value (first boot, or a
// whitespace-only file) leaves the env seed in place. A hard read failure aborts
// construction, so a broken persistence volume surfaces at startup.
func (s *RefreshingTokenSource) seedFromStore() error {
	if s.store == nil {
		return nil
	}
	persisted, err := s.loadLocked()
	if err != nil {
		return fmt.Errorf("goapi: loading persisted refresh token: %w", err)
	}
	if persisted != "" {
		s.refreshTok = persisted
		s.storedTok = persisted
	}
	return nil
}

func (s *RefreshingTokenSource) loadLocked() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeLockWait)
	defer cancel()
	release, err := s.store.lock(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return s.store.load()
}

// Token returns a live read-only access token, refreshing when none is cached or
// the proactive window has elapsed. Concurrent callers whose token is due share a
// single exchange; a caller whose ctx ends first abandons the wait without
// aborting the refresh the others still need.
func (s *RefreshingTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	if tok := s.cachedLocked(); tok != "" {
		s.mu.Unlock()
		return tok, nil
	}
	if s.inflight == nil && s.inBackoffLocked() {
		err := s.lastErr
		s.mu.Unlock()
		return "", err
	}
	call := s.joinRefreshLocked()
	s.mu.Unlock()

	select {
	case <-call.done:
		return call.token, call.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// inBackoffLocked reports whether a failed exchange's backoff window is still
// open, so the next exchange must be withheld rather than storming the endpoint.
// When it is open s.lastErr is the non-nil typed failure to surface. Caller holds
// s.mu. It never withholds an in-flight exchange — only the start of a new one.
func (s *RefreshingTokenSource) inBackoffLocked() bool {
	return s.now().Before(s.retryAt)
}

// cachedLocked returns the cached access token while it is still inside its
// proactive-refresh window, or "" when none is cached or a refresh is due. Caller
// holds s.mu.
func (s *RefreshingTokenSource) cachedLocked() string {
	if s.accessToken == "" || !s.now().Before(s.refreshAt) {
		return ""
	}
	return s.accessToken
}

// joinRefreshLocked returns the in-flight refresh, starting one if none runs.
// Exactly one goroutine performs the exchange and every concurrent caller shares
// its result — the single-flight that collapses N buckets' due refreshes into one
// network call. Caller holds s.mu.
func (s *RefreshingTokenSource) joinRefreshLocked() *refreshCall {
	if s.inflight != nil {
		return s.inflight
	}
	call := &refreshCall{done: make(chan struct{})}
	s.inflight = call
	go s.runRefresh(call)
	return call
}

// runRefresh performs one token exchange for the shared call, stores a successful
// result, clears the single-flight slot and publishes to every waiter. It uses a
// detached, timeout-bounded context so one caller cancelling never aborts the
// refresh the others are waiting on.
func (s *RefreshingTokenSource) runRefresh(call *refreshCall) {
	token, err := s.advanceChain()

	s.mu.Lock()
	if err == nil {
		s.resetBackoffLocked()
	} else {
		s.recordFailureLocked(err)
	}
	s.inflight = nil
	s.mu.Unlock()

	call.token, call.err = token, err
	close(call.done)
}

func (s *RefreshingTokenSource) advanceChain() (string, error) {
	if s.store == nil {
		return s.exchangeHeld()
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeLockWait)
	defer cancel()
	release, err := s.store.lock(ctx)
	if err != nil {
		return "", &TokenRefreshError{Stage: "lock", Err: err}
	}
	defer release()

	if _, err := s.adoptStoredIf(s.rotatedElsewhereLocked); err != nil {
		return "", &TokenRefreshError{Stage: "load", Err: err}
	}
	token, err := s.exchangeHeld()
	if !isSpentRefreshToken(err) {
		return token, err
	}
	adopted, loadErr := s.adoptStoredIf(s.differsFromHeldLocked)
	if loadErr != nil || !adopted {
		return token, err
	}
	return s.exchangeHeld()
}

func (s *RefreshingTokenSource) exchangeHeld() (string, error) {
	s.mu.Lock()
	held := s.refreshTok
	s.mu.Unlock()

	token, refreshAt, rotated, err := s.exchange(context.Background(), held)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.accessToken = token
	s.refreshAt = refreshAt
	if rotated != "" {
		s.refreshTok = rotated
	}
	s.mu.Unlock()

	if rotated != "" {
		s.persistRotation(rotated)
	}
	return token, nil
}

func (s *RefreshingTokenSource) adoptStoredIf(shouldAdoptLocked func(stored string) bool) (bool, error) {
	stored, err := s.store.load()
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored == "" || !shouldAdoptLocked(stored) {
		return false, nil
	}
	s.refreshTok = stored
	s.storedTok = stored
	slog.Info("goapi: adopted the refresh token persisted by another holder", "endpoint", s.endpoint)
	return true, nil
}

func (s *RefreshingTokenSource) rotatedElsewhereLocked(stored string) bool {
	return stored != s.storedTok
}

func (s *RefreshingTokenSource) differsFromHeldLocked(stored string) bool {
	return stored != s.refreshTok
}

func isSpentRefreshToken(err error) bool {
	var refreshErr *TokenRefreshError
	return errors.As(err, &refreshErr) && refreshErr.Status == http.StatusBadRequest
}

// resetBackoffLocked clears the backoff after a successful exchange so a recovered
// endpoint — or a reseed picked up after restart — refreshes at once rather than
// waiting out a stale window. Caller holds s.mu.
func (s *RefreshingTokenSource) resetBackoffLocked() {
	s.failCount = 0
	s.retryAt = time.Time{}
	s.lastErr = nil
}

// recordFailureLocked pushes the next-allowed-exchange deadline out on the capped
// exponential curve and stores the typed failure to surface while the window is
// open. The cap is finite, so retryAt is always eventually in the past and the
// next Token attempts a fresh exchange — the property that keeps a terminal 400
// from wedging a later valid token. It logs the backoff (never the token: err is a
// *TokenRefreshError carrying no secret) so operators see backoff, not a 429 storm.
// Caller holds s.mu.
func (s *RefreshingTokenSource) recordFailureLocked(err error) {
	s.failCount++
	wait := s.backoff.Backoff(s.failCount)
	s.retryAt = s.now().Add(wait)
	s.lastErr = err
	slog.Warn("goapi: read-only token refresh failed, backing off",
		"endpoint", s.endpoint,
		"consecutive_failures", s.failCount,
		"retry_in", wait,
		"error", err,
	)
}

func (s *RefreshingTokenSource) persistRotation(rotated string) {
	if s.store == nil {
		return
	}
	err := s.store.save(rotated)

	s.mu.Lock()
	s.persistFailed = err != nil
	if err == nil {
		s.storedTok = rotated
	}
	s.mu.Unlock()

	if err != nil {
		slog.Warn("goapi: persisting rotated refresh token failed", "endpoint", s.endpoint, "error", err)
	}
}

// exchange performs one refresh-token grant and returns the new access token, its
// proactive-refresh deadline and the rotated refresh token. Every failure is a
// *TokenRefreshError carrying no token material.
func (s *RefreshingTokenSource) exchange(ctx context.Context, refreshTok string) (string, time.Time, string, error) {
	req, err := s.buildRequest(ctx, refreshTok)
	if err != nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "build", Err: err}
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "transport", Err: err}
	}
	if resp == nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "transport", Err: errors.New("nil response")}
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, maxTokenResponseBytes)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "status", Status: resp.StatusCode, Err: errRefreshRejected}
	}
	return s.parseExchange(resp.Body)
}

// buildRequest assembles the POST to the token endpoint. The refresh token rides
// in the JSON body and the anon key in the apikey header; neither is placed in the
// URL, so neither can leak through a request log that records only the line.
func (s *RefreshingTokenSource) buildRequest(ctx context.Context, refreshTok string) (*http.Request, error) {
	body, err := json.Marshal(refreshGrantBody{RefreshToken: refreshTok})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", s.anonKey)
	return req, nil
}

// parseExchange decodes a bounded token response and derives the proactive-refresh
// deadline from the access token's own exp claim.
func (s *RefreshingTokenSource) parseExchange(body io.Reader) (string, time.Time, string, error) {
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(body, maxTokenResponseBytes)).Decode(&tr); err != nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "decode", Err: err}
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "decode", Err: errMissingAccessToken}
	}
	exp, err := accessTokenExpiry(tr.AccessToken)
	if err != nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "claims", Err: err}
	}
	deadline, err := s.proactiveDeadline(exp)
	if err != nil {
		return "", time.Time{}, "", &TokenRefreshError{Stage: "claims", Err: err}
	}
	return tr.AccessToken, deadline, tr.RefreshToken, nil
}

// proactiveDeadline places the refresh at 4/5 of the token's remaining lifetime,
// clamped so an absurd exp neither overflows the multiply nor pins the cache and a
// tiny exp cannot spin the refresh hot.
func (s *RefreshingTokenSource) proactiveDeadline(exp time.Time) (time.Time, error) {
	now := s.now()
	lifetime := exp.Sub(now)
	if lifetime <= 0 {
		return time.Time{}, errExpiredAccessToken
	}
	if lifetime > maxAcceptedLifetime {
		lifetime = maxAcceptedLifetime
	}
	window := lifetime / refreshLeadDen * refreshLeadNum
	if window < minRefreshInterval {
		window = minRefreshInterval
	}
	return now.Add(window), nil
}

// invalidate discards the cached access token so the next Token() forces a fresh
// exchange. The client calls it once on a 401: a token go-api rejected before its
// proactive-refresh window (early revocation, clock skew) is dropped rather than
// re-presented. The refresh token is untouched — only an exchange rotates it.
func (s *RefreshingTokenSource) invalidate() {
	s.mu.Lock()
	s.accessToken = ""
	s.refreshAt = time.Time{}
	s.mu.Unlock()
}

// String renders the source without its secrets, so a %s/%v of it — or of a struct
// that embeds it — can never spill a token.
func (s *RefreshingTokenSource) String() string {
	return "goapi.RefreshingTokenSource{endpoint:" + s.endpoint + "}"
}

// LogValue redacts all token material when the source is logged through slog,
// reporting only the non-secret endpoint and whether a token is currently cached.
func (s *RefreshingTokenSource) LogValue() slog.Value {
	s.mu.Lock()
	cached := s.accessToken != ""
	backingOff := s.inBackoffLocked()
	failures := s.failCount
	persistFailed := s.persistFailed
	s.mu.Unlock()
	return slog.GroupValue(
		slog.String("endpoint", s.endpoint),
		slog.Bool("has_cached_token", cached),
		slog.Bool("persisted", s.store != nil),
		slog.Bool("persist_failed", persistFailed),
		slog.Bool("backing_off", backingOff),
		slog.Int("consecutive_failures", failures),
	)
}

// refreshGrantBody is the token-exchange request body.
type refreshGrantBody struct {
	RefreshToken string `json:"refresh_token"`
}

// tokenResponse is the subset of the Supabase token response Overseer reads. The
// rotated refresh_token is persisted in memory; expires_in is ignored in favour of
// the access token's own exp claim, so the refresh clock matches exactly what
// go-api verifies.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// accessTokenExpiry reads the exp claim from a JWT access token WITHOUT verifying
// its signature: the token arrived over TLS from the configured Supabase endpoint,
// and go-api — not this client — is the party that verifies it. Only exp is read,
// to schedule the proactive refresh; no token bytes are logged or returned in any
// error.
func accessTokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errMalformedAccessToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, errMalformedAccessToken
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, errMalformedAccessToken
	}
	if claims.Exp <= 0 {
		return time.Time{}, errMissingExpClaim
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}

// tokenRefresher is the optional capability a TokenSource exposes when its token
// can go stale before its proactive-refresh window (early revocation, clock skew):
// the client discards the cached token on a 401 so the next request re-fetches. A
// StaticTokenSource does not implement it — a static 401 is a genuine rejection,
// not a staleness a refresh can fix.
type tokenRefresher interface {
	invalidate()
}

// invalidateOn401 discards a refreshing source's cached token when go-api rejected
// it (401), so the next request presents a fresh token rather than re-sending one
// the server already refused. A non-401 status, or a source that cannot refresh,
// is a no-op. It never logs, returns or otherwise touches the token value.
func invalidateOn401(tokens TokenSource, status int) {
	if status != http.StatusUnauthorized {
		return
	}
	if r, ok := tokens.(tokenRefresher); ok {
		r.invalidate()
	}
}

// nullTokenSource is the fail-closed source selected when Overseer has no read-only
// credentials configured: every Token() returns ErrNoToken, so the client sends no
// unauthenticated request and buckets degrade to source-down instead of panicking.
type nullTokenSource struct{}

func (nullTokenSource) Token(context.Context) (string, error) { return "", ErrNoToken }

// sharedOnce memoizes the process-wide source so every bucket that calls
// SharedTokenSource shares one instance.
var (
	sharedOnce   sync.Once
	sharedSource TokenSource
)

// SharedTokenSource returns the process-wide read-only TokenSource selected from
// the environment, constructed once and memoized so every bucket that adopts it
// shares ONE instance — which is what makes the refreshing source's single-flight
// span buckets (one refresh for the whole fleet, not one per bucket). Selection:
//
//   - OVERSEER_SUPABASE_URL + OVERSEER_SUPABASE_ANON_KEY +
//     OVERSEER_GOAPI_READONLY_REFRESH_TOKEN all set -> a RefreshingTokenSource
//     (stays live past the ~1h access-token TTL);
//   - else OVERSEER_GOAPI_READONLY_TOKEN set -> a StaticTokenSource (fixed JWT);
//   - else a nullTokenSource that fails closed.
//
// There is deliberately no operator fallback: with no read-only credential the
// buckets degrade to source-down, which is the point of #1810.
//
// Buckets still read OVERSEER_GOAPI_URL themselves for the go-api base; this
// governs only which credential the shared client presents.
func SharedTokenSource() TokenSource {
	sharedOnce.Do(func() { sharedSource = selectTokenSource(os.Getenv) })
	return sharedSource
}

// selectTokenSource is SharedTokenSource's pure core: it reads config through
// getenv and returns the selected source, logging (never the secret) and failing
// closed to a null source when refresh vars are present but malformed.
func selectTokenSource(getenv func(string) string) TokenSource {
	warnLegacyOperatorCredentials(getenv)
	supaURL := strings.TrimSpace(getenv(envSupabaseURL))
	anonKey := strings.TrimSpace(getenv(envSupabaseAnon))
	refreshTok := strings.TrimSpace(getenv(envReadOnlyRefreshToken))

	if supaURL != "" && anonKey != "" && refreshTok != "" {
		store := fileRefreshTokenStore{path: refreshTokenPath(getenv)}
		src, err := NewRefreshingTokenSource(supaURL, anonKey, refreshTok, WithRefreshTokenStore(store))
		if err != nil {
			// Fail closed rather than silently downgrade: the operator explicitly
			// configured refresh, so a bad URL must surface, not fall back to a
			// static token that may be absent or stale.
			slog.Warn(logRefreshSource, "mode", "refreshing", "status", "invalid config, degrading to source-down", "error", err)
			return nullTokenSource{}
		}
		slog.Info(logRefreshSource, "mode", "refreshing", "persist_path", store.path)
		return src
	}

	if token := strings.TrimSpace(getenv(envReadOnlyToken)); token != "" {
		slog.Info(logRefreshSource, "mode", "static")
		return StaticTokenSource(token)
	}

	slog.Warn(logRefreshSource, "mode", "null", "status", "no read-only credentials configured, buckets degrade to source-down")
	return nullTokenSource{}
}

// warnLegacyOperatorCredentials names a leftover operator credential in the
// environment so an upgraded deployment shows why its buckets went source-down,
// rather than looking like an outage. It is a diagnostic only: the value is read
// for presence and never used, since an operator token would restore the write
// scope #1810 removed.
func warnLegacyOperatorCredentials(getenv func(string) string) {
	for _, name := range []string{envLegacyOperatorRefreshToken, envLegacyOperatorToken} {
		if strings.TrimSpace(getenv(name)) != "" {
			slog.Warn(logRefreshSource, "ignored_var", name,
				"status", "operator credential ignored; set the OVERSEER_GOAPI_READONLY_* vars instead")
		}
	}
}

// refreshTokenPath resolves where the rotating read-only refresh token is
// persisted: the OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE override when set,
// else the default volume path.
func refreshTokenPath(getenv func(string) string) string {
	if p := strings.TrimSpace(getenv(envReadOnlyRefreshFile)); p != "" {
		return p
	}
	return defaultRefreshTokenPath
}
