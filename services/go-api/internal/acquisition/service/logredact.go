package service

import "altune/go-api/internal/shared/redact"

func logSafeError(err error) string { return redact.LogError(err) }

func logSafeText(s string) string { return redact.LogText(s) }
