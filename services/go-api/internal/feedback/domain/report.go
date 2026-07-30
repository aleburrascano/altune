package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"altune/go-api/internal/shared"
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string   { return e.Message }
func (e *ValidationError) HTTPStatus() int { return 400 }

type Kind int

const (
	KindBug Kind = iota
	KindIdea
	KindConfusing
)

func (k Kind) String() string {
	switch k {
	case KindIdea:
		return "idea"
	case KindConfusing:
		return "confusing"
	default:
		return "bug"
	}
}

func (k Kind) Label() string {
	switch k {
	case KindIdea:
		return "enhancement"
	case KindConfusing:
		return "ux"
	default:
		return "bug"
	}
}

func ParseKind(s string) (Kind, error) {
	switch s {
	case "bug":
		return KindBug, nil
	case "idea":
		return KindIdea, nil
	case "confusing":
		return KindConfusing, nil
	default:
		return KindBug, &ValidationError{Message: fmt.Sprintf("unknown kind: %q", s)}
	}
}

const (
	MinMessageRunes = 10
	MaxMessageRunes = 2000
	maxTitleRunes   = 72
	maxDiagRunes    = 64
)

type Diagnostics struct {
	AppVersion string
	Platform   string
	OSVersion  string
	Screen     string
}

func (d Diagnostics) sanitized() Diagnostics {
	return Diagnostics{
		AppVersion: singleLine(d.AppVersion),
		Platform:   singleLine(d.Platform),
		OSVersion:  singleLine(d.OSVersion),
		Screen:     singleLine(d.Screen),
	}
}

func singleLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, maxDiagRunes)
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:limit-1])) + "…"
}

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
		return nil, &ValidationError{Message: "report needs a reporter"}
	}
	return &Report{
		Reporter:    reporter,
		Kind:        kind,
		Message:     message,
		Diagnostics: diag.sanitized(),
		SubmittedAt: time.Now().UTC(),
	}, nil
}

func validateMessage(message string) error {
	count := utf8.RuneCountInString(message)
	if count < MinMessageRunes {
		return &ValidationError{Message: fmt.Sprintf("describe it in at least %d characters", MinMessageRunes)}
	}
	if count > MaxMessageRunes {
		return &ValidationError{Message: fmt.Sprintf("keep it under %d characters", MaxMessageRunes)}
	}
	return nil
}

func (r *Report) Title() string {
	first := r.Message
	if line, _, found := strings.Cut(first, "\n"); found {
		first = strings.TrimSpace(line)
	}
	return fmt.Sprintf("[%s] %s", r.Kind, truncate(first, maxTitleRunes))
}
