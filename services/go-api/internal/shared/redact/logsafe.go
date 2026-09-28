package redact

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var logTokenRe = regexp.MustCompile("[^\\s'\"`()\\[\\]{}<>,;=|]+")

var cookieFileRe = regexp.MustCompile(`(?i)cookie[^\\/]*\.(?:txt|json|sqlite|db|jar|cookies)$`)

var quotedPathRe = regexp.MustCompile(`'(?:/|[A-Za-z]:\\)[^'\n]*'|"(?:/|[A-Za-z]:\\)[^"\n]*"`)

var windowsAbsRe = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\)`)

const redactedPath = "[REDACTED]"

func LogError(err error) string {
	if err == nil {
		return ""
	}
	return LogText(err.Error())
}

func LogText(s string) string {
	m := &pathMasker{tmp: filepath.Clean(os.TempDir())}
	s = quotedPathRe.ReplaceAllStringFunc(Secrets(s), m.maskQuoted)
	return logTokenRe.ReplaceAllStringFunc(s, m.mask)
}

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

func (m *pathMasker) maskQuoted(quoted string) string {
	inner := quoted[1 : len(quoted)-1]
	if isCookiePath(inner) || m.isForeignAbsPath(inner) {
		return quoted[:1] + redactedPath + quoted[:1]
	}
	return quoted
}

func looksLikePath(tok string) bool {
	return strings.ContainsAny(tok, `/\.`)
}

func isCookiePath(tok string) bool {
	lower := strings.ToLower(tok)
	if isWebURL(lower) || strings.HasPrefix(tok, "--") || !strings.Contains(lower, "cookie") {
		return false
	}
	return strings.ContainsAny(tok, `/\`) || cookieFileRe.MatchString(tok)
}

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
