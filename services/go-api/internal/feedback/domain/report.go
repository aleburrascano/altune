package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"altune/go-api/internal/shared"
)

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

// Valid reports whether k is one of the defined kinds. String and Label fall
// back to "bug" for anything else, so an unchecked Kind would mislabel quietly.
func (k Kind) Valid() bool {
	switch k {
	case KindBug, KindIdea, KindConfusing:
		return true
	default:
		return false
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
		return KindBug, NewValidationError(fmt.Sprintf("unknown kind: %q", truncate(s, maxKindEchoRunes)))
	}
}

const (
	MinMessageRunes = 10
	MaxMessageRunes = 2000
	maxTitleRunes   = 72
	maxDiagRunes    = 64
	// maxKindEchoRunes bounds how much of a rejected kind is reflected back in
	// the validation error, so an oversized value is not echoed near-verbatim.
	maxKindEchoRunes = 16
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
		return nil, NewValidationError("report needs a reporter")
	}
	if !kind.Valid() {
		return nil, NewValidationError(fmt.Sprintf("unknown kind: %d", int(kind)))
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
	if visibleRuneCount(message) < MinMessageRunes {
		return NewValidationError(fmt.Sprintf("describe it in at least %d characters", MinMessageRunes))
	}
	if utf8.RuneCountInString(message) > MaxMessageRunes {
		return NewValidationError(fmt.Sprintf("keep it under %d characters", MaxMessageRunes))
	}
	return nil
}

// visibleRuneCount counts the runes a reader would see: invisible runes
// (format characters such as U+200B, non-whitespace controls) never count, and
// whitespace counts only between visible runes, so an invisible-only or
// invisibly padded message cannot reach the minimum.
func visibleRuneCount(message string) int {
	count := 0
	for _, r := range strings.TrimFunc(message, isBlank) {
		if !isInvisible(r) {
			count++
		}
	}
	return count
}

func isBlank(r rune) bool { return unicode.IsSpace(r) || isInvisible(r) }

func isInvisible(r rune) bool {
	return unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && !unicode.IsSpace(r))
}

func (r *Report) Title() string {
	first := r.Message
	if line, _, found := strings.Cut(first, "\n"); found {
		first = strings.TrimSpace(line)
	}
	return fmt.Sprintf("[%s] %s", r.Kind, truncate(first, maxTitleRunes))
}
