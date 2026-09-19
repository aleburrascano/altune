package service

import "altune/go-api/internal/shared/redact"

// logSafeError and logSafeText are this package's names for the log redaction
// that now lives in internal/shared/redact, where the ytdlp and streamrip
// adapters can reach it too: their log sites mask the same cookie jar paths and
// subprocess stderr these do.
func logSafeError(err error) string { return redact.LogError(err) }

func logSafeText(s string) string { return redact.LogText(s) }
