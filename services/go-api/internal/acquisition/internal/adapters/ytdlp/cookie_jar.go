package ytdlp

import (
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

const firstSignInFailureLog = "acquisition.cookie_session_first_signin_failure"

var sessionCookies = []string{
	"LOGIN_INFO", "SID", "HSID", "SSID", "APISID", "SAPISID",
	"__Secure-1PAPISID", "__Secure-3PAPISID", "__Secure-1PSID", "__Secure-3PSID", "__Secure-1PSIDTS",
}

type cookieLease struct {
	live        string
	run         string
	environment string
	path        string
	snapshot    []byte
	cleanup     func()
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
}

type signInFailure struct {
	At              time.Time `json:"at"`
	SessionAgeHours float64   `json:"session_age_hours"`
	Run             string    `json:"run"`
	WriteBacks      int       `json:"write_backs"`
	Environment     string    `json:"environment"`
}

func (s *YtDlpAudioSearcher) leaseCookieJar(run string) (cookieLease, error) {
	if s.cookieFile == "" {
		return cookieLease{cleanup: func() {}}, nil
	}
	snapshot, err := os.ReadFile(s.cookieFile)
	if err != nil {
		return cookieLease{}, err
	}
	path, cleanup, err := writeTempCopy(snapshot, "acquisition-"+run+"-cookies-*.txt")
	if err != nil {
		return cookieLease{}, err
	}
	return cookieLease{live: s.cookieFile, run: run, environment: s.environment, path: path, snapshot: snapshot, cleanup: cleanup}, nil
}

func (l cookieLease) settle(ctx context.Context, runErr error, stderr string) {
	if l.live == "" {
		return
	}
	var err error
	switch {
	case runErr == nil:
		err = l.keepRenewedCookies(ctx)
	case asksToSignIn(stderr):
		err = l.recordSignInFailure(ctx)
	}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.cookie_jar_update_failed", "run", l.run, "error", redact.LogError(err))
	}
}

func asksToSignIn(stderr string) bool {
	return strings.Contains(stderr, "not a bot")
}

func (l cookieLease) keepRenewedCookies(ctx context.Context) error {
	renewed, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	if bytes.Equal(renewed, l.snapshot) || dropsSessionCookie(l.snapshot, renewed) {
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

func (l cookieLease) recordSignInFailure(ctx context.Context) error {
	return l.underLock(ctx, func(_ jarFile, session jarSession) error {
		if session.FirstSignInFailure != nil {
			return nil
		}
		session.FirstSignInFailure = l.signInFailure(session, time.Now())
		if err := session.save(l.live); err != nil {
			return err
		}
		logFirstSignInFailure(ctx, session)
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
	unlock, err := lockJar(ctx, l.live+cookieJarLockSuffix)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := readJarFile(l.live)
	if err != nil || !bytes.Equal(current.contents, l.snapshot) {
		return err
	}
	return update(current, loadSession(l.live).following(current))
}

func readJarFile(path string) (jarFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return jarFile{}, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return jarFile{}, err
	}
	contents, err := io.ReadAll(f)
	return jarFile{contents: contents, mode: info.Mode().Perm(), modTime: info.ModTime()}, err
}

func loadSession(live string) jarSession {
	var session jarSession
	raw, _ := os.ReadFile(live + cookieJarSessionSuffix)
	_ = json.Unmarshal(raw, &session)
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
		return err
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
		return err
	}
	_, writeErr := f.Write(contents)
	if err := errors.Join(writeErr, f.Chmod(mode), f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(next, path)
}

func lockJar(ctx context.Context, path string) (unlock func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, lockWaitLimit)
	defer cancel()
	for {
		unlock, err := tryLockJar(path)
		if !errors.Is(err, errJarBusy) {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("gave up waiting for the cookie jar lock: %w", ctx.Err())
		case <-time.After(lockRetryInterval):
		}
	}
}

func dropsSessionCookie(snapshot, renewed []byte) bool {
	had, kept := youtubeCookieNames(snapshot), youtubeCookieNames(renewed)
	return slices.ContainsFunc(sessionCookies, func(name string) bool { return had[name] && !kept[name] })
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
