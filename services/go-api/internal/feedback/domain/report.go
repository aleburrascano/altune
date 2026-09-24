package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinMessageRunes  = 10
	MaxMessageRunes  = 2000
	maxTitleRunes    = 72
	maxDiagRunes     = 64
	maxKindEchoRunes = 16
)

type Report struct {
	Reporter    shared.UserId
	Kind        Kind
	Message     string
	Diagnostics Diagnostics
	SubmittedAt time.Time
}

func NewReport(reporter shared.UserId, kind Kind, message string, diag Diagnostics) (*Report, error) {
	message = strings.TrimSpace(message)
	if err := validateMessage(message); err != nil {
		return nil, err
	}
	if reporter.IsZero() {
		return nil, NewValidationError("report needs a reporter")
	}
	if !kind.Valid() {
		return nil, NewValidationError(fmt.Sprintf("unknown kind: %d", int(kind)))
	}
	return &Report{
		Reporter:    reporter,
		Kind:        kind,
		Message:     redactSecrets(message),
		Diagnostics: diag.sanitized(),
		SubmittedAt: time.Now().UTC(),
	}, nil
}

func validateMessage(message string) error {
	if visibleRuneCount(message) < MinMessageRunes {
		return NewValidationError(fmt.Sprintf("describe it in at least %d characters", MinMessageRunes))
	}
	if utf8.RuneCountInString(message) > MaxMessageRunes {
		return NewValidationError(fmt.Sprintf("keep it under %d characters", MaxMessageRunes))
	}
	return nil
}

func (r *Report) Title() string {
	return fmt.Sprintf("[%s] %s", r.Kind, truncate(firstVisibleLine(r.Message), maxTitleRunes))
}

func firstVisibleLine(message string) string {
	for line := range strings.Lines(message) {
		if text := visibleText(line); text != "" {
			return text
		}
	}
	return ""
}
