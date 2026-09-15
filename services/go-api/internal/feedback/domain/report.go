package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Kind int

const (
	KindBug Kind = iota
	KindIdea
	KindConfusing
)

// kinds is the single source of truth for every defined Kind: it maps each to
// its wire/display name. String, Valid, and ParseKind all derive from it, so a
// new Kind needs exactly one entry here.
var kinds = map[Kind]string{
	KindBug:       "bug",
	KindIdea:      "idea",
	KindConfusing: "confusing",
}

// String returns the kind's name, or "Kind(N)" for an undefined kind so it is
// never mistaken for a real one.
func (k Kind) String() string {
	if name, ok := kinds[k]; ok {
		return name
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Valid reports whether k is one of the defined kinds.
func (k Kind) Valid() bool {
	_, ok := kinds[k]
	return ok
}

func ParseKind(s string) (Kind, error) {
	for kind, name := range kinds {
		if name == s {
			return kind, nil
		}
	}
	return KindBug, NewValidationError(fmt.Sprintf("unknown kind: %q", truncate(s, maxKindEchoRunes)))
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

// NewDiagnostics assembles into the domain type the diagnostics an adapter
// collected. It is the one construction site the DTO→domain mapping goes
// through, so the adapter never hand-lists the fields itself.
func NewDiagnostics(appVersion, platform, osVersion, screen string) Diagnostics {
	return Diagnostics{
		AppVersion: appVersion,
		Platform:   platform,
		OSVersion:  osVersion,
		Screen:     screen,
	}
}

// diagnosticsFields enumerates every Diagnostics field exactly once as an
// accessor/mutator pair — the single place that iterates the fields. sanitized
// ranges over it, so a new field is sanitized the moment it joins the list, and
// TestDiagnostics_SanitizesEveryField fails loudly if the list falls behind the
// struct.
var diagnosticsFields = []struct {
	get func(Diagnostics) string
	set func(*Diagnostics, string)
}{
	{func(d Diagnostics) string { return d.AppVersion }, func(d *Diagnostics, v string) { d.AppVersion = v }},
	{func(d Diagnostics) string { return d.Platform }, func(d *Diagnostics, v string) { d.Platform = v }},
	{func(d Diagnostics) string { return d.OSVersion }, func(d *Diagnostics, v string) { d.OSVersion = v }},
	{func(d Diagnostics) string { return d.Screen }, func(d *Diagnostics, v string) { d.Screen = v }},
}

func (d Diagnostics) sanitized() Diagnostics {
	var out Diagnostics
	for _, f := range diagnosticsFields {
		f.set(&out, singleLine(f.get(d)))
	}
	return out
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

// visibleText drops every invisible rune (directional overrides, zero-width
// characters) and collapses whitespace, so text shown as a title reads exactly
// as a person sees it and cannot be visually spoofed.
func visibleText(s string) string {
	s = strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func (r *Report) Title() string {
	first := r.Message
	if line, _, found := strings.Cut(first, "\n"); found {
		first = strings.TrimSpace(line)
	}
	first = visibleText(first)
	return fmt.Sprintf("[%s] %s", r.Kind, truncate(first, maxTitleRunes))
}
