package redact

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// logTokenRe splits diagnostic text into the tokens a filesystem path or flag
// value can occupy. Whitespace, quotes, brackets, and "=" end a token, so
// "--cookies=/x" and "path=/x" expose their value as a token of its own.
var logTokenRe = regexp.MustCompile("[^\\s'\"`()\\[\\]{}<>,;=|]+")

// cookieFileRe matches a bare file name that names a cookie jar
// (cookies.txt, yt_cookies.json), which carries no separator of its own.
var cookieFileRe = regexp.MustCompile(`(?i)cookie[^\\/]*\.(?:txt|json|sqlite|db|jar|cookies)$`)

// quotedPathRe matches a quoted absolute path, which may contain spaces that
// would otherwise split it into tokens the per-token rules cannot recognise.
var quotedPathRe = regexp.MustCompile(`'(?:/|[A-Za-z]:\\)[^'\n]*'|"(?:/|[A-Za-z]:\\)[^"\n]*"`)

// windowsAbsRe matches a drive-rooted or UNC Windows path.
var windowsAbsRe = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\)`)

const redactedPath = "[REDACTED]"

// LogError renders err for a log attribute with credentials and host
// filesystem layout removed. A raw subprocess error chain embeds stderr
// verbatim, and yt-dlp names its --cookies file there (ARCHITECTURE §2.7). The
// acquisition failure reason already guards the stored/wire copy; this guards
// the log copy, extending Secrets' URL scrub to argv and paths.
func LogError(err error) string {
	if err == nil {
		return ""
	}
	return LogText(err.Error())
}

// LogText applies LogError's redaction to arbitrary diagnostic text, such as a
// subprocess's stderr or a recovered panic value. Token by token it masks the
// value of a --cookies flag, any path or file name naming a cookie jar, and any
// absolute filesystem path outside the OS temp dir (the per-job scratch dirs
// are the only layout worth keeping for triage). http(s) URLs are left intact
// apart from Secrets' query-param scrub.
func LogText(s string) string {
	m := &pathMasker{tmp: filepath.Clean(os.TempDir())}
	s = quotedPathRe.ReplaceAllStringFunc(Secrets(s), m.maskQuoted)
	return logTokenRe.ReplaceAllStringFunc(s, m.mask)
}

// pathMasker carries the one token of lookbehind needed to spot a --cookies
// flag's value; ReplaceAllStringFunc visits tokens left to right.
type pathMasker struct {
	tmp             string
	afterCookieFlag bool
}

func (m *pathMasker) mask(tok string) string {
	afterFlag := m.afterCookieFlag
	m.afterCookieFlag = strings.EqualFold(tok, "--cookies")
	if (afterFlag && looksLikePath(tok)) || isCookiePath(tok) || m.isForeignAbsPath(tok) {
		return redactedPath
	}
	return tok
}

// maskQuoted masks a whole quoted absolute path, spaces included, keeping the
// quotes so the surrounding message still reads naturally.
func (m *pathMasker) maskQuoted(quoted string) string {
	inner := quoted[1 : len(quoted)-1]
	if isCookiePath(inner) || m.isForeignAbsPath(inner) {
		return quoted[:1] + redactedPath + quoted[:1]
	}
	return quoted
}

// looksLikePath reports whether tok is a filesystem path or file name rather
// than prose, so "--cookies for the authentication" keeps its wording.
func looksLikePath(tok string) bool {
	return strings.ContainsAny(tok, `/\.`)
}

// isCookiePath reports whether tok is a filesystem path or file name (not an
// http(s) URL or a flag) that names a cookie jar or a directory holding one.
func isCookiePath(tok string) bool {
	lower := strings.ToLower(tok)
	if isWebURL(lower) || strings.HasPrefix(tok, "--") || !strings.Contains(lower, "cookie") {
		return false
	}
	return strings.ContainsAny(tok, `/\`) || cookieFileRe.MatchString(tok)
}

// isForeignAbsPath reports whether tok is an absolute filesystem path (or a
// file: URL) that does not resolve inside the OS temp dir.
func (m *pathMasker) isForeignAbsPath(tok string) bool {
	if strings.HasPrefix(strings.ToLower(tok), "file:") {
		return true
	}
	if windowsAbsRe.MatchString(tok) {
		return true
	}
	if !strings.HasPrefix(tok, "/") || len(tok) == 1 {
		return false
	}
	clean := filepath.Clean(tok)
	return clean != m.tmp && !strings.HasPrefix(clean, m.tmp+string(filepath.Separator))
}

func isWebURL(lower string) bool {
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
