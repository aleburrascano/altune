package domain

import (
	"regexp"
)

const RedactedMarker = "[REDACTED]"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`),
	regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}`),
	regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`),
	regexp.MustCompile(`\b[rs]k_live_[0-9A-Za-z]{16,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)[A-Za-z0-9\-._~+/]{16,}=*`),
	regexp.MustCompile(`(?i)(\b(?:password|passwd|pwd|secret|client[_-]?secret|api[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|private[_-]?key)["']?\s*[:=]\s*["']?)[^\s"',;]+`),
}

func redactSecrets(message string) string {
	for _, p := range secretPatterns {
		if p.NumSubexp() > 0 {
			message = p.ReplaceAllString(message, "${1}"+RedactedMarker)
		} else {
			message = p.ReplaceAllLiteralString(message, RedactedMarker)
		}
	}
	return message
}
