package ytdlp

import (
	"altune/go-api/internal/shared/redact"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
)

const firstSignInFailureLog = "acquisition.cookie_session_first_signin_failure"

var sapisidCookies = []string{"SAPISID", "__Secure-1PAPISID", "__Secure-3PAPISID"}

type cookieLease struct {
	live     string
	run      string
	path     string
	snapshot []byte
	cleanup  func()
}

type jarFile struct {
	contents []byte
	mode     fs.FileMode
	modTime  time.Time
}

type jarSession struct {
	JarSHA256           string    `json:"jar_sha256"`
	InstalledAt         time.Time `json:"installed_at"`
	WriteBacks          int       `json:"write_backs"`
	SignInFailureLogged bool      `json:"signin_failure_logged"`
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
	return cookieLease{live: s.cookieFile, run: run, path: path, snapshot: snapshot, cleanup: cleanup}, nil
}

func (l cookieLease) settle(ctx context.Context, runErr error, stderr string) {
	if l.live == "" {
		return
	}
	var err error
	switch {
	case runErr == nil:
		err = l.keepRenewedCookies()
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

func (l cookieLease) keepRenewedCookies() error {
	renewed, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	if bytes.Equal(renewed, l.snapshot) || signedIn(l.snapshot) && !signedIn(renewed) {
		return nil
	}
	return l.underLock(func(current jarFile, session jarSession) error {
		if err := replaceFile(l.live, renewed, current.mode); err != nil {
			return err
		}
		session.JarSHA256 = digest(renewed)
		session.WriteBacks++
		return session.save(l.live)
	})
}

func (l cookieLease) recordSignInFailure(ctx context.Context) error {
	return l.underLock(func(_ jarFile, session jarSession) error {
		if session.SignInFailureLogged {
			return nil
		}
		slog.WarnContext(ctx, firstSignInFailureLog,
			"run", l.run,
			"session_age_hours", math.Round(time.Since(session.InstalledAt).Hours()*10)/10,
			"installed_at", session.InstalledAt.Format(time.RFC3339),
			"write_backs", session.WriteBacks)
		session.SignInFailureLogged = true
		return session.save(l.live)
	})
}

func (l cookieLease) underLock(update func(jarFile, jarSession) error) error {
	unlock, err := lockJar(l.live + cookieJarLockSuffix)
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

func signedIn(jar []byte) bool {
	names := youtubeCookieNames(jar)
	return names["LOGIN_INFO"] && slices.ContainsFunc(sapisidCookies, func(name string) bool { return names[name] })
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
