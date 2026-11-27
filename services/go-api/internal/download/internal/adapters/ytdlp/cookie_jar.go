package ytdlp

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	cookieJarLockSuffix    = ".lock"
	cookieJarSessionSuffix = ".session.json"
	replacementSuffix      = ".next"
	sessionFileMode        = 0o644
	lockRetryInterval      = 25 * time.Millisecond
	lockWaitLimit          = 3 * time.Second
)

const (
	firstSignInFailureLog = "acquisition.cookie_session_first_signin_failure"
	renewalRefusedLog     = "acquisition.cookie_renewal_refused"
)

const youTubeSearchPrefix = "ytsearch"

var youTubeTargetHosts = map[string]bool{
	"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true, "music.youtube.com": true, "youtu.be": true,
}

var sessionCookies = []string{
	"LOGIN_INFO", "SID", "HSID", "SSID", "APISID", "SAPISID",
	"__Secure-1PAPISID", "__Secure-3PAPISID", "__Secure-1PSID", "__Secure-3PSID", "__Secure-1PSIDTS",
}

type cookieLease struct {
	live        string
	run         string
	environment string
	youtube     bool
	installedAt time.Time
	path        string
	snapshot    []byte
	cleanup     func()
	now         clock.Func
	sleep       clock.Sleeper
}

type jarFile struct {
	contents []byte
	mode     fs.FileMode
	modTime  time.Time
}

type jarSession struct {
	JarSHA256          string         `json:"jar_sha256"`
	InstalledAt        time.Time      `json:"installed_at"`
	WriteBacks         int            `json:"write_backs"`
	FirstSignInFailure *signInFailure `json:"first_signin_failure"`
	SignedOutSince     *time.Time     `json:"signed_out_since"`
}

type signInFailure struct {
	At              time.Time `json:"at"`
	SessionAgeHours float64   `json:"session_age_hours"`
	Run             string    `json:"run"`
	WriteBacks      int       `json:"write_backs"`
	Environment     string    `json:"environment"`
}

func (s *YouTubeAudioSearcher) SignedOutSince(ctx context.Context) (time.Time, bool) {
	if s.cookieFile == "" {
		return time.Time{}, false
	}
	var since *time.Time
	err := cookieLease{live: s.cookieFile, now: s.now, sleep: s.sleep}.locked(ctx, func(current jarFile) error {
		since = loadSession(ctx, s.cookieFile).following(current).SignedOutSince
		return nil
	})
	if err != nil || since == nil {
		return time.Time{}, false
	}
	return *since, true
}

func (s *YouTubeAudioSearcher) leaseCookieJar(ctx context.Context, run, target string) (cookieLease, error) {
	if s.cookieFile == "" {
		return cookieLease{cleanup: func() {}}, nil
	}
	live, err := readJarFile(s.cookieFile)
	if err != nil {
		return cookieLease{}, err
	}
	path, cleanup, err := writeTempCopy(live.contents, "acquisition-"+run+"-cookies-*.txt")
	if err != nil {
		return cookieLease{}, err
	}
	return cookieLease{
		live: s.cookieFile, run: run, environment: s.environment,
		youtube: isYouTubeTarget(target), installedAt: loadSession(ctx, s.cookieFile).following(live).InstalledAt,
		path: path, snapshot: live.contents, cleanup: cleanup, now: s.now, sleep: s.sleep,
	}, nil
}

func isYouTubeTarget(target string) bool {
	if strings.HasPrefix(target, youTubeSearchPrefix) {
		return true
	}
	u, err := url.Parse(target)
	return err == nil && youTubeTargetHosts[strings.ToLower(u.Hostname())]
}

func (l cookieLease) settle(ctx context.Context, runErr error, stderr string) {
	if l.live == "" {
		return
	}
	var err error
	switch {
	case runErr == nil:
		err = errors.Join(l.keepRenewedCookies(ctx), l.markSignedIn(ctx))
	case asksToSignIn(stderr):
		err = l.recordSignInFailure(ctx)
	}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.cookie_jar_update_failed", "run", l.run, logging.KeyError, redact.LogError(err))
	}
}

func asksToSignIn(stderr string) bool {
	return strings.Contains(stderr, "not a bot")
}

func (l cookieLease) keepRenewedCookies(ctx context.Context) error {
	renewed, err := os.ReadFile(l.path)
	if err != nil {
		return fmt.Errorf("read the renewed cookies: %w", err)
	}
	if bytes.Equal(renewed, l.snapshot) {
		return nil
	}
	if dropped := droppedSessionCookies(l.snapshot, renewed); len(dropped) > 0 {
		slog.InfoContext(ctx, renewalRefusedLog, "run", l.run, "missing_cookies", dropped)
		return nil
	}
	return l.underLock(ctx, func(current jarFile, session jarSession) error {
		if err := replaceFile(l.live, renewed, current.mode); err != nil {
			return err
		}
		session.JarSHA256 = digest(renewed)
		session.WriteBacks++
		return session.save(l.live)
	})
}

func (l cookieLease) markSignedIn(ctx context.Context) error {
	if !l.youtube || loadSession(ctx, l.live).SignedOutSince == nil {
		return nil
	}
	return l.underSessionLock(ctx, func(session jarSession) error {
		if session.SignedOutSince == nil {
			return nil
		}
		session.SignedOutSince = nil
		return session.save(l.live)
	})
}

func (l cookieLease) recordSignInFailure(ctx context.Context) error {
	return l.underSessionLock(ctx, func(session jarSession) error {
		first := session.FirstSignInFailure == nil
		if !first && session.SignedOutSince != nil {
			return nil
		}
		now := l.now()
		if first {
			session.FirstSignInFailure = l.signInFailure(session, now)
		}
		if session.SignedOutSince == nil {
			session.SignedOutSince = &now
		}
		if err := session.save(l.live); err != nil {
			return err
		}
		if first {
			logFirstSignInFailure(ctx, session)
		}
		return nil
	})
}

func (l cookieLease) signInFailure(session jarSession, now time.Time) *signInFailure {
	return &signInFailure{
		At:              now,
		SessionAgeHours: math.Round(now.Sub(session.InstalledAt).Hours()*10) / 10,
		Run:             l.run,
		WriteBacks:      session.WriteBacks,
		Environment:     l.environment,
	}
}

func logFirstSignInFailure(ctx context.Context, session jarSession) {
	failure := session.FirstSignInFailure
	slog.WarnContext(ctx, firstSignInFailureLog,
		"run", failure.Run,
		"environment", failure.Environment,
		"session_age_hours", failure.SessionAgeHours,
		"installed_at", session.InstalledAt.Format(time.RFC3339),
		"write_backs", failure.WriteBacks)
}

func (l cookieLease) underLock(ctx context.Context, update func(jarFile, jarSession) error) error {
	return l.locked(ctx, func(current jarFile) error {
		if !bytes.Equal(current.contents, l.snapshot) {
			return nil
		}
		return update(current, loadSession(ctx, l.live).following(current))
	})
}

func (l cookieLease) underSessionLock(ctx context.Context, update func(jarSession) error) error {
	return l.locked(ctx, func(current jarFile) error {
		session := loadSession(ctx, l.live).following(current)
		if !session.InstalledAt.Equal(l.installedAt) {
			return nil
		}
		return update(session)
	})
}

func (l cookieLease) locked(ctx context.Context, update func(jarFile) error) error {
	unlock, err := lockJar(ctx, l.sleep, l.live+cookieJarLockSuffix)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := readJarFile(l.live)
	if err != nil {
		return err
	}
	return update(current)
}

func readJarFile(path string) (jarFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return jarFile{}, fmt.Errorf("open the cookie jar: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return jarFile{}, fmt.Errorf("stat the cookie jar: %w", err)
	}
	contents, err := io.ReadAll(f)
	if err != nil {
		return jarFile{}, fmt.Errorf("read the cookie jar: %w", err)
	}
	return jarFile{contents: contents, mode: info.Mode().Perm(), modTime: info.ModTime()}, nil
}

func loadSession(ctx context.Context, live string) jarSession {
	var session jarSession
	raw, _ := os.ReadFile(live + cookieJarSessionSuffix)
	if len(raw) == 0 {
		return session
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		slog.ErrorContext(ctx, "ytdlp.cookie_session_unreadable", logging.KeyError, err)
		return jarSession{}
	}
	return session
}

func (s jarSession) following(current jarFile) jarSession {
	sum := digest(current.contents)
	if s.JarSHA256 == sum {
		return s
	}
	return jarSession{JarSHA256: sum, InstalledAt: current.modTime.UTC()}
}

func (s jarSession) save(live string) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode the cookie session: %w", err)
	}
	return replaceFile(live+cookieJarSessionSuffix, raw, sessionFileMode)
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func replaceFile(path string, contents []byte, mode fs.FileMode) error {
	next := path + replacementSuffix
	_ = os.Remove(next)
	f, err := os.OpenFile(next, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create the replacement file: %w", err)
	}
	_, writeErr := f.Write(contents)
	if err := errors.Join(writeErr, f.Chmod(mode), f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("write the replacement file: %w", err)
	}
	if err := os.Rename(next, path); err != nil {
		return fmt.Errorf("move the replacement into place: %w", err)
	}
	return nil
}

func lockJar(ctx context.Context, sleep clock.Sleeper, path string) (unlock func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, lockWaitLimit)
	defer cancel()
	for {
		unlock, err := tryLockJar(path)
		if !errors.Is(err, errJarBusy) {
			return unlock, err
		}
		if err := sleep(ctx, lockRetryInterval); err != nil {
			return nil, fmt.Errorf("gave up waiting for the cookie jar lock: %w", err)
		}
	}
}

func droppedSessionCookies(snapshot, renewed []byte) []string {
	had, kept := youtubeCookieNames(snapshot), youtubeCookieNames(renewed)
	return slices.DeleteFunc(slices.Clone(sessionCookies), func(name string) bool { return !had[name] || kept[name] })
}

func youtubeCookieNames(jar []byte) map[string]bool {
	names := map[string]bool{}
	for line := range strings.Lines(string(jar)) {
		fields := strings.Split(line, "\t")
		if len(fields) == 7 && isYouTubeHost(fields[0]) {
			names[fields[5]] = true
		}
	}
	return names
}

func isYouTubeHost(domain string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(domain, "#HttpOnly_"), ".")
	return host == "youtube.com" || host == "www.youtube.com"
}
