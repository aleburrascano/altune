package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const RedactedMarker = "[REDACTED]"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`),
	regexp.MustCompile(`(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
	regexp.MustCompile(`xox[abposr]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
	regexp.MustCompile(`[rs]k_live_[0-9A-Za-z]{16,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)[A-Za-z0-9\-._~+/]{16,}=*`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`npm_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)(\bauthorization["']?\s*:\s*["']?token\s+)[^\s"',;]+`),
	regexp.MustCompile(`(?i)(\btoken["']?\s*=\s*["']?)[^\s"',;]+`),
	regexp.MustCompile(`(?i)(\b(?:[A-Za-z0-9]+_)*(?:` + secretLabelWords + `)(?:_[A-Za-z0-9]+)*["']?\s*[:=]\s*["']?)[^\s"',;]+`),
}

const secretLabelWords = `password|passwd|pwd|secret|client[_-]?secret|api[_-]?key|private[_-]?key|` +
	`(?:access|auth|refresh)[_-]?token|[A-Za-z0-9]+_token`

func redactSecrets(message string) string {
	for _, p := range secretPatterns {
		message = redactPattern(p, message)
	}
	return message
}

func redactPattern(p *regexp.Regexp, message string) string {
	visible, offsets := visibleProjection(message)
	var out strings.Builder
	last := 0
	for _, m := range p.FindAllStringSubmatchIndex(visible, -1) {
		start, end := secretSpan(m, offsets)
		out.WriteString(message[last:start] + RedactedMarker)
		last = end
	}
	return out.String() + message[last:]
}

func secretSpan(m, offsets []int) (start, end int) {
	start = m[0]
	if len(m) > 2 {
		start = m[3]
	}
	return offsets[start], offsets[m[1]-1] + 1
}

func visibleProjection(s string) (visible string, offsets []int) {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isInvisible(r) {
			b.WriteString(s[i : i+size])
			offsets = appendByteOffsets(offsets, i, size)
		}
		i += size
	}
	return b.String(), offsets
}

func appendByteOffsets(offsets []int, at, size int) []int {
	for k := range size {
		offsets = append(offsets, at+k)
	}
	return offsets
}
