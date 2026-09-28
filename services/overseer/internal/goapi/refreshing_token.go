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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const refreshGrantPath = "/auth/v1/token"

const maxTokenResponseBytes = 1 << 16

const refreshHTTPTimeout = 10 * time.Second

const (
	storeLockWait  = 3 * refreshHTTPTimeout
	storeLockRetry = 50 * time.Millisecond
)

const (
	refreshLeadNum = 4
	refreshLeadDen = 5
)

const maxAcceptedLifetime = 24 * time.Hour

const minRefreshInterval = 5 * time.Second

const (
	refreshBackoffBase = 1 * time.Second
	refreshBackoffMax  = 5 * time.Minute
)

const (
	envSupabaseURL          = "OVERSEER_SUPABASE_URL"
	envSupabaseAnon         = "OVERSEER_SUPABASE_ANON_KEY"
	envReadOnlyRefreshToken = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN"
	envReadOnlyRefreshFile  = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE"
	envReadOnlyToken        = "OVERSEER_GOAPI_READONLY_TOKEN"
	envReadOnlyEmail        = "OVERSEER_GOAPI_READONLY_EMAIL"
	envReadOnlyPassword     = "OVERSEER_GOAPI_READONLY_PASSWORD"
	logRefreshSource        = "goapi: token source selection"
)

const (
	envLegacyOperatorRefreshToken = "OVERSEER_GOAPI_REFRESH_TOKEN"
	envLegacyOperatorToken        = "OVERSEER_GOAPI_TOKEN"
)

const defaultRefreshTokenPath = "/var/lib/overseer/readonly_refresh_token"

var (
	errMalformedAccessToken = errors.New("malformed access token")
	errMissingExpClaim      = errors.New("access token missing exp claim")
	errExpiredAccessToken   = errors.New("access token already expired")
	errMissingAccessToken   = errors.New("token response missing access_token")
	errRefreshRejected      = errors.New("token endpoint rejected the refresh grant")
	errStoreLockUnavailable = errors.New("refresh token file lock not acquired")
	errNoRefreshToken       = errors.New("no refresh token held")
)

type TokenRefreshError struct {
	Stage  string
	Status int
	Err    error
}

func (e *TokenRefreshError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("goapi: read-only token refresh failed at %s: status %d", e.Stage, e.Status)
	}
	return fmt.Sprintf("goapi: read-only token refresh failed at %s: %v", e.Stage, e.Err)
}

func (e *TokenRefreshError) Unwrap() error { return e.Err }

type RefreshingTokenSource struct {
	endpoint       string
	signInEndpoint string
	signIn         *passwordCredentials
	anonKey        string
	http           *http.Client
	now            func() time.Time

	store refreshTokenStore

	mu          sync.Mutex
	accessToken string
	refreshTok  string
	refreshAt   time.Time
	expAt       time.Time
	inflight    *refreshCall

	backoff   Backoff
	failCount int
	retryAt   time.Time
	lastErr   error

	storedTok     string
	persistFailed bool
	refreshedAt   time.Time
}

type refreshCall struct {
	done  chan struct{}
	token string
	err   error
}

type RefreshingOption func(*RefreshingTokenSource)

func WithRefreshHTTPClient(h *http.Client) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if h != nil {
			s.http = h
		}
	}
}

type passwordCredentials struct {
	email    string
	password string
}

func WithPasswordGrant(email, password string) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if !passwordGrantComplete(email, password) {
			return
		}
		s.signIn = &passwordCredentials{email: strings.TrimSpace(email), password: password}
	}
}

func passwordGrantComplete(email, password string) bool {
	return strings.TrimSpace(email) != "" && password != ""
}

func withClock(now func() time.Time) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if now != nil {
			s.now = now
		}
	}
}

func WithRefreshTokenStore(store refreshTokenStore) RefreshingOption {
	return func(s *RefreshingTokenSource) {
		if store != nil {
			s.store = store
		}
	}
}

type refreshTokenStore interface {
	lock(ctx context.Context) (release func(), err error)
	load() (string, error)
	save(token string) error
}

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
	defer func() { _ = os.Remove(tmpName) }()
	if err := writeTokenFile(tmp, token); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

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

func NewRefreshingTokenSource(supabaseURL, anonKey, refreshToken string, opts ...RefreshingOption) (*RefreshingTokenSource, error) {
	base, err := parseBaseURL(supabaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(anonKey) == "" {
		return nil, errors.New("goapi: empty Supabase anon key")
	}

	s := &RefreshingTokenSource{
		endpoint:       grantEndpoint(base, "refresh_token"),
		signInEndpoint: grantEndpoint(base, "password"),
		anonKey:        anonKey,
		refreshTok:     strings.TrimSpace(refreshToken),
		http:           &http.Client{Timeout: refreshHTTPTimeout, CheckRedirect: refuseRedirect},
		now:            time.Now,
		backoff:        NewExpBackoff(refreshBackoffBase, refreshBackoffMax),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.refreshTok == "" && s.signIn == nil {
		return nil, errors.New("goapi: empty Supabase refresh token")
	}
	if err := s.seedFromStore(); err != nil {
		return nil, err
	}
	return s, nil
}

func grantEndpoint(base *url.URL, grantType string) string {
	endpoint := base.JoinPath(refreshGrantPath)
	q := endpoint.Query()
	q.Set("grant_type", grantType)
	endpoint.RawQuery = q.Encode()
	return endpoint.String()
}

func (s *RefreshingTokenSource) seedFromStore() error {
	if s.store == nil {
		return nil
	}
	release, err := s.lockStore()
	if err != nil {
		warnUnpersisted(s.endpoint, err)
		return nil
	}
	defer release()
	persisted, err := s.store.load()
	if err != nil {
		warnUnpersisted(s.endpoint, err)
		s.persistFailed = true
		return nil
	}
	if persisted != "" {
		s.refreshTok = persisted
		s.storedTok = persisted
	}
	return nil
}

func (s *RefreshingTokenSource) lockStore() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeLockWait)
	defer cancel()
	return s.store.lock(ctx)
}

func warnUnpersisted(endpoint string, err error) {
	slog.Warn("goapi: refresh token file unusable, continuing unpersisted", "endpoint", endpoint, "error", err)
}

func isLockContention(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errStoreLockUnavailable)
}

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
		if call.err == nil {
			return call.token, nil
		}
		s.mu.Lock()
		tok := s.cachedLocked()
		s.mu.Unlock()
		if tok != "" {
			return tok, nil
		}
		return "", call.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *RefreshingTokenSource) inBackoffLocked() bool {
	return s.now().Before(s.retryAt)
}

func (s *RefreshingTokenSource) cachedLocked() string {
	if s.accessToken == "" || !s.now().Before(s.expAt) {
		return ""
	}
	if s.now().Before(s.refreshAt) || s.inBackoffLocked() {
		return s.accessToken
	}
	return ""
}

func (s *RefreshingTokenSource) joinRefreshLocked() *refreshCall {
	if s.inflight != nil {
		return s.inflight
	}
	call := &refreshCall{done: make(chan struct{})}
	s.inflight = call
	go s.runRefresh(call)
	return call
}

func (s *RefreshingTokenSource) runRefresh(call *refreshCall) {
	token, endpoint, err := s.advanceChain()

	s.mu.Lock()
	if err == nil {
		s.resetBackoffLocked()
		s.refreshedAt = s.now()
	} else {
		s.recordFailureLocked(endpoint, err)
	}
	s.inflight = nil
	s.mu.Unlock()

	call.token, call.err = token, err
	close(call.done)
}

func (s *RefreshingTokenSource) advanceChain() (string, string, error) {
	if s.store == nil {
		return s.signInIfSpent(s.exchangeHeld())
	}
	release, err := s.lockStore()
	if isLockContention(err) {
		return "", s.endpoint, &TokenRefreshError{Stage: "lock", Err: err}
	}
	if err != nil {
		warnUnpersisted(s.endpoint, err)
		return s.signInIfSpent(s.exchangeHeld())
	}
	defer release()
	return s.signInIfSpent(s.exchangeAdoptingStored())
}

func (s *RefreshingTokenSource) exchangeAdoptingStored() (string, error) {
	s.adoptStoredIf(s.rotatedElsewhereLocked)
	token, err := s.exchangeHeld()
	if !isSpentRefreshToken(err) {
		return token, err
	}
	if !s.adoptStoredIf(s.differsFromHeldLocked) {
		return token, err
	}
	return s.exchangeHeld()
}

func (s *RefreshingTokenSource) signInIfSpent(token string, err error) (string, string, error) {
	if s.signIn == nil || !needsSignIn(err) {
		return token, s.endpoint, err
	}
	body := passwordGrantBody{Email: s.signIn.email, Password: s.signIn.password}
	token, refreshAt, expAt, rotated, err := s.exchange(context.Background(), s.signInEndpoint, body)
	if err != nil {
		return "", s.signInEndpoint, asSignInFailure(err)
	}
	s.adoptExchange(token, refreshAt, expAt, rotated)
	slog.Info("goapi: read-only account signed in again with the password grant", "endpoint", s.signInEndpoint)
	return token, s.signInEndpoint, nil
}

func needsSignIn(err error) bool {
	return isSpentRefreshToken(err) || errors.Is(err, errNoRefreshToken)
}

func asSignInFailure(err error) error {
	var refreshErr *TokenRefreshError
	if !errors.As(err, &refreshErr) {
		return &TokenRefreshError{Stage: "password_grant", Err: err}
	}
	return &TokenRefreshError{Stage: "password_grant", Status: refreshErr.Status, Err: refreshErr.Err}
}

func (s *RefreshingTokenSource) exchangeHeld() (string, error) {
	s.mu.Lock()
	held := s.refreshTok
	s.mu.Unlock()
	if held == "" {
		return "", &TokenRefreshError{Stage: "seed", Err: errNoRefreshToken}
	}

	token, refreshAt, expAt, rotated, err := s.exchange(context.Background(), s.endpoint, refreshGrantBody{RefreshToken: held})
	if err != nil {
		return "", err
	}
	s.adoptExchange(token, refreshAt, expAt, rotated)
	return token, nil
}

func (s *RefreshingTokenSource) adoptExchange(token string, refreshAt, expAt time.Time, rotated string) {
	s.mu.Lock()
	s.accessToken = token
	s.refreshAt = refreshAt
	s.expAt = expAt
	if rotated != "" {
		s.refreshTok = rotated
	}
	s.mu.Unlock()

	if rotated != "" {
		s.persistRotation(rotated)
	}
}

func (s *RefreshingTokenSource) adoptStoredIf(shouldAdoptLocked func(stored string) bool) bool {
	stored, err := s.store.load()
	if err != nil {
		warnUnpersisted(s.endpoint, err)
		s.mu.Lock()
		s.persistFailed = true
		s.mu.Unlock()
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored == "" || !shouldAdoptLocked(stored) {
		return false
	}
	s.refreshTok = stored
	s.storedTok = stored
	slog.Info("goapi: adopted the refresh token persisted by another holder", "endpoint", s.endpoint)
	return true
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

func (s *RefreshingTokenSource) resetBackoffLocked() {
	s.failCount = 0
	s.retryAt = time.Time{}
	s.lastErr = nil
}

func (s *RefreshingTokenSource) recordFailureLocked(endpoint string, err error) {
	s.failCount++
	wait := s.backoff.Backoff(s.failCount)
	s.retryAt = s.now().Add(wait)
	s.lastErr = err
	slog.Warn("goapi: read-only token refresh failed, backing off",
		"endpoint", endpoint,
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

func (s *RefreshingTokenSource) exchange(ctx context.Context, endpoint string, grant any) (string, time.Time, time.Time, string, error) {
	req, err := s.buildRequest(ctx, endpoint, grant)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "build", Err: err}
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "transport", Err: err}
	}
	if resp == nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "transport", Err: errors.New("nil response")}
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, maxTokenResponseBytes)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "status", Status: resp.StatusCode, Err: errRefreshRejected}
	}
	return s.parseExchange(resp.Body)
}

func (s *RefreshingTokenSource) buildRequest(ctx context.Context, endpoint string, grant any) (*http.Request, error) {
	body, err := json.Marshal(grant)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", s.anonKey)
	return req, nil
}

func (s *RefreshingTokenSource) parseExchange(body io.Reader) (string, time.Time, time.Time, string, error) {
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(body, maxTokenResponseBytes)).Decode(&tr); err != nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "decode", Err: err}
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "decode", Err: errMissingAccessToken}
	}
	exp, err := accessTokenExpiry(tr.AccessToken)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "claims", Err: err}
	}
	deadline, err := s.proactiveDeadline(exp)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", &TokenRefreshError{Stage: "claims", Err: err}
	}
	return tr.AccessToken, deadline, exp, tr.RefreshToken, nil
}

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

func (s *RefreshingTokenSource) invalidateRejected(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rejected != s.accessToken {
		return
	}
	s.dropCachedLocked()
}

func (s *RefreshingTokenSource) dropCachedLocked() {
	s.accessToken = ""
	s.refreshAt = time.Time{}
	s.expAt = time.Time{}
}

func (s *RefreshingTokenSource) String() string {
	return "goapi.RefreshingTokenSource{endpoint:" + s.endpoint + "}"
}

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
		slog.Bool("password_grant", s.signIn != nil),
		slog.Bool("backing_off", backingOff),
		slog.Int("consecutive_failures", failures),
	)
}

type refreshGrantBody struct {
	RefreshToken string `json:"refresh_token"`
}

type passwordGrantBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

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

type tokenRefresher interface {
	invalidateRejected(rejected string)
}

func invalidateOn401(tokens TokenSource, status int, rejected string) {
	if status != http.StatusUnauthorized {
		return
	}
	if r, ok := tokens.(tokenRefresher); ok {
		r.invalidateRejected(rejected)
	}
}

type nullTokenSource struct{}

func (nullTokenSource) Token(context.Context) (string, error) { return "", ErrNoToken }

var (
	sharedOnce   sync.Once
	sharedSource TokenSource
)

func SharedTokenSource() TokenSource {
	sharedOnce.Do(func() { sharedSource = selectTokenSource(os.Getenv) })
	return sharedSource
}

func selectTokenSource(getenv func(string) string) TokenSource {
	warnLegacyOperatorCredentials(getenv)
	supaURL := strings.TrimSpace(getenv(envSupabaseURL))
	anonKey := strings.TrimSpace(getenv(envSupabaseAnon))
	refreshTok := strings.TrimSpace(getenv(envReadOnlyRefreshToken))
	signIn := WithPasswordGrant(getenv(envReadOnlyEmail), getenv(envReadOnlyPassword))
	hasSignIn := passwordGrantComplete(getenv(envReadOnlyEmail), getenv(envReadOnlyPassword))

	if supaURL != "" && anonKey != "" && (refreshTok != "" || hasSignIn) {
		store := fileRefreshTokenStore{path: refreshTokenPath(getenv)}
		src, err := NewRefreshingTokenSource(supaURL, anonKey, refreshTok, WithRefreshTokenStore(store), signIn)
		if err != nil {
			slog.Warn(logRefreshSource, "mode", "refreshing", "status", "invalid config, degrading to source-down", "error", err)
			return nullTokenSource{}
		}
		slog.Info(logRefreshSource, "mode", "refreshing", "persist_path", store.path, "password_grant", hasSignIn)
		return src
	}

	if token := strings.TrimSpace(getenv(envReadOnlyToken)); token != "" {
		slog.Info(logRefreshSource, "mode", "static")
		return StaticTokenSource(token)
	}

	slog.Warn(logRefreshSource, "mode", "null", "status", "no read-only credentials configured, buckets degrade to source-down")
	return nullTokenSource{}
}

func warnLegacyOperatorCredentials(getenv func(string) string) {
	for _, name := range []string{envLegacyOperatorRefreshToken, envLegacyOperatorToken} {
		if strings.TrimSpace(getenv(name)) != "" {
			slog.Warn(logRefreshSource, "ignored_var", name,
				"status", "operator credential ignored; set the OVERSEER_GOAPI_READONLY_* vars instead")
		}
	}
}

func refreshTokenPath(getenv func(string) string) string {
	if p := strings.TrimSpace(getenv(envReadOnlyRefreshFile)); p != "" {
		return p
	}
	return defaultRefreshTokenPath
}
