package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func reporter() shared.UserId { return shared.NewUserId(uuid.New()) }

func TestKind_ExistingKindsKeepNamesAndRoundTrip(t *testing.T) {
	cases := map[Kind]string{KindBug: "bug", KindIdea: "idea", KindConfusing: "confusing"}
	for kind, name := range cases {
		if kind.String() != name {
			t.Fatalf("%d.String() = %q, want %q", int(kind), kind.String(), name)
		}
		parsed, err := ParseKind(name)
		if err != nil || parsed != kind {
			t.Fatalf("ParseKind(%q) = %v, %v; want %d", name, parsed, err, int(kind))
		}
	}
}

func TestKind_UnknownIsExplicitNotBug(t *testing.T) {
	for _, kind := range []Kind{Kind(-1), KindConfusing + 1, Kind(99)} {
		if kind.Valid() {
			t.Fatalf("Kind(%d).Valid() = true, want false", int(kind))
		}
		if got := kind.String(); got == KindBug.String() || !strings.HasPrefix(got, "Kind(") {
			t.Fatalf("Kind(%d).String() = %q, want an explicit Kind(N)", int(kind), got)
		}
	}
}

func TestParseKind_RejectsUnknown(t *testing.T) {
	if _, err := ParseKind("rant"); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestParseKind_BoundsEchoedValue(t *testing.T) {
	_, err := ParseKind(strings.Repeat("x", 10000))
	if err == nil {
		t.Fatal("expected an oversized kind to be rejected")
	}
	if len(err.Error()) > 128 {
		t.Fatalf("error is %d bytes, want the oversized kind truncated", len(err.Error()))
	}
}

func TestParseKind_EchoesShortUnknownValue(t *testing.T) {
	_, err := ParseKind("rant")
	if err == nil || !strings.Contains(err.Error(), `unknown kind: "rant"`) {
		t.Fatalf("err = %v, want the short value echoed", err)
	}
}

func TestNewReport_RejectsTooShortMessage(t *testing.T) {
	if _, err := NewReport(reporter(), KindBug, "broken", Diagnostics{}); err == nil {
		t.Fatal("expected a too-short message to be rejected")
	}
}

// Invisible runes spelled by code point so the source stays plain ASCII.
var (
	zeroWidthSpace  = string(rune(0x200B))
	zeroWidthJoiner = string(rune(0x200D))
	wordJoiner      = string(rune(0x2060))
	byteOrderMark   = string(rune(0xFEFF))
	softHyphen      = string(rune(0x00AD))
)

func TestNewReport_RejectsMessageWithoutVisibleContent(t *testing.T) {
	cases := map[string]string{
		"zero-width spaces":            strings.Repeat(zeroWidthSpace, MinMessageRunes),
		"mixed format runes":           strings.Repeat(zeroWidthSpace+zeroWidthJoiner+wordJoiner+byteOrderMark+softHyphen, MinMessageRunes),
		"control runes":                strings.Repeat("\x01", MinMessageRunes),
		"spaces padded by zero-widths": zeroWidthSpace + strings.Repeat(" ", MinMessageRunes) + zeroWidthSpace,
		"short text padded invisibly":  "broken" + strings.Repeat(zeroWidthSpace, MinMessageRunes),
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewReport(reporter(), KindBug, message, Diagnostics{})
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), "at least") {
				t.Fatalf("err = %v, want the too-short validation error", err)
			}
		})
	}
}

// TestNewReport_RejectsMessageOfBlankNonFormatRunes reproduces #1108: runes
// that render as nothing but sit outside category Cf (Hangul fillers are Lo,
// variation selectors are Mn) were counted as visible, so a message built only
// from them passed validation.
func TestNewReport_RejectsMessageOfBlankNonFormatRunes(t *testing.T) {
	cases := map[string]rune{
		"hangul jungseong filler":    0x1160,
		"hangul choseong filler":     0x115F,
		"hangul filler":              0x3164,
		"halfwidth hangul filler":    0xFFA0,
		"combining grapheme joiner":  0x034F,
		"variation selector-16":      0xFE0F,
		"mongolian vowel separator":  0x180E,
		"braille pattern blank":      0x2800,
		"khmer inherent vowel aq":    0x17B4,
		"tag space":                  0xE0020,
		"supplementary var selector": 0xE0100,
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			message := strings.Repeat(string(r), MinMessageRunes*2)
			_, err := NewReport(reporter(), KindBug, message, Diagnostics{})
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), "at least") {
				t.Fatalf("err = %v, want the too-short validation error", err)
			}
		})
	}
}

func TestNewReport_CountsInteriorSpacesTowardTheMinimum(t *testing.T) {
	if _, err := NewReport(reporter(), KindBug, "it crashes", Diagnostics{}); err != nil {
		t.Fatalf("NewReport: %v, want a 10-character message with an interior space accepted", err)
	}
}

func TestNewReport_RejectsTooLongMessage(t *testing.T) {
	_, err := NewReport(reporter(), KindBug, strings.Repeat("a", MaxMessageRunes+1), Diagnostics{})
	if err == nil {
		t.Fatal("expected an over-long message to be rejected")
	}
}

func TestNewReport_RejectsMissingReporter(t *testing.T) {
	if _, err := NewReport(shared.UserId{}, KindBug, "the downloads screen is empty", Diagnostics{}); err == nil {
		t.Fatal("expected a missing reporter to be rejected")
	}
}

func TestNewReport_RejectsOutOfRangeKind(t *testing.T) {
	for _, kind := range []Kind{Kind(-1), KindConfusing + 1, Kind(99)} {
		report, err := NewReport(reporter(), kind, "the downloads screen is empty", Diagnostics{})
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("NewReport(kind=%d) = %+v, %v; want a validation error", int(kind), report, err)
		}
		if report != nil {
			t.Fatalf("NewReport(kind=%d) returned a report alongside the error", int(kind))
		}
	}
}

func TestNewReport_AcceptsEveryValidKind(t *testing.T) {
	for _, kind := range []Kind{KindBug, KindIdea, KindConfusing} {
		report, err := NewReport(reporter(), kind, "the downloads screen is empty", Diagnostics{})
		if err != nil {
			t.Fatalf("NewReport(kind=%s): %v", kind, err)
		}
		if report.Kind != kind {
			t.Fatalf("report.Kind = %s, want %s", report.Kind, kind)
		}
	}
}

func TestNewReport_TrimsMessage(t *testing.T) {
	report, err := NewReport(reporter(), KindIdea, "   let me sort by year   ", Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if report.Message != "let me sort by year" {
		t.Fatalf("message = %q, want it trimmed", report.Message)
	}
}

// Secret fixtures are assembled at runtime so no literal credential shape sits
// in the source for a secret scanner to flag.
func TestNewReport_RedactsPastedSecrets(t *testing.T) {
	cases := map[string]struct{ secret, keep string }{
		"aws access key":    {"AKIA" + strings.Repeat("Q", 16), ""},
		"bearer token":      {strings.Repeat("aB3", 12), "Authorization: Bearer "},
		"github token":      {"ghp_" + strings.Repeat("x9", 18), ""},
		"github pat":        {"github_pat_" + strings.Repeat("a1_", 10), ""},
		"slack token":       {"xoxb-" + strings.Repeat("12ab-", 4), ""},
		"google api key":    {"AIza" + strings.Repeat("k", 35), ""},
		"stripe key":        {"sk_" + "live_" + strings.Repeat("z", 24), ""},
		"jwt":               {"eyJ" + strings.Repeat("h", 12) + ".eyJ" + strings.Repeat("p", 12) + "." + strings.Repeat("s", 12), ""},
		"password assign":   {"hunter2hunter2", "password="},
		"quoted api key":    {"s3cr3tv4lu3", `"api_key": "`},
		"private key block": {"-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("M", 40) + "\n-----END RSA PRIVATE KEY-----", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			message := "playback broke, log says " + tc.keep + tc.secret + " then it crashed"
			report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
			if err != nil {
				t.Fatalf("NewReport: %v", err)
			}
			if strings.Contains(report.Message, tc.secret) {
				t.Fatalf("message = %q, secret passed through unchanged", report.Message)
			}
			want := "playback broke, log says " + tc.keep + RedactedMarker
			if !strings.HasPrefix(report.Message, want) || !strings.HasSuffix(report.Message, " then it crashed") {
				t.Fatalf("message = %q, want prefix %q and the surrounding text kept", report.Message, want)
			}
			if strings.Contains(report.Title(), tc.secret) {
				t.Fatalf("title = %q, secret leaked into the title", report.Title())
			}
		})
	}
}

func TestNewReport_LeavesOrdinaryProseUnredacted(t *testing.T) {
	for _, message := range []string{
		"the password reset screen never loads on my phone",
		"basic functionality like shuffle is broken since 2.3.1",
		"track AKIRA theme by Geinoh Yamashirogumi shows wrong artist",
		"token expired error on /api/v1/library/tracks after an hour",
	} {
		report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
		if err != nil {
			t.Fatalf("NewReport(%q): %v", message, err)
		}
		if report.Message != message {
			t.Fatalf("message = %q, want %q unchanged", report.Message, message)
		}
	}
}

func TestNewReport_FlattensDiagnosticsToOneLine(t *testing.T) {
	report, err := NewReport(reporter(), KindBug, "three tracks went grey", Diagnostics{
		Screen: "settings\n\n| injected | table |",
	})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if strings.Contains(report.Diagnostics.Screen, "\n") {
		t.Fatalf("screen = %q, want newlines collapsed", report.Diagnostics.Screen)
	}
}

// TestDiagnostics_SanitizesEveryField dirties every string field via reflection
// and asserts the enumeration in diagnosticsFields cleaned each one. A field
// added to the struct but omitted from that list fails here rather than
// silently skipping sanitization.
func TestDiagnostics_SanitizesEveryField(t *testing.T) {
	var dirty Diagnostics
	dv := reflect.ValueOf(&dirty).Elem()
	for i := range dv.NumField() {
		dv.Field(i).SetString("multi\nline   value")
	}

	clean := reflect.ValueOf(dirty.sanitized())
	for i := range clean.NumField() {
		name := clean.Type().Field(i).Name
		if got := clean.Field(i).String(); strings.ContainsAny(got, "\n\t") || strings.Contains(got, "  ") {
			t.Fatalf("field %s not sanitized: %q", name, got)
		}
	}
}

// TestNewDiagnostics_PopulatesEveryField guards the DTO→domain construction
// site: every field must be assigned, so a field added to the struct but missed
// in NewDiagnostics fails loudly here instead of being dropped silently.
func TestNewDiagnostics_PopulatesEveryField(t *testing.T) {
	diag := reflect.ValueOf(NewDiagnostics("app", "platform", "os", "screen"))
	for i := range diag.NumField() {
		if diag.Field(i).String() == "" {
			t.Fatalf("NewDiagnostics leaves %s empty; every field must be mapped", diag.Type().Field(i).Name)
		}
	}
}

func TestReportTitle_PrefixesKindAndTruncates(t *testing.T) {
	report, err := NewReport(reporter(), KindBug, strings.Repeat("long ", 40), Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	title := report.Title()
	if !strings.HasPrefix(title, "[bug] ") {
		t.Fatalf("title = %q, want a [bug] prefix", title)
	}
	if len([]rune(title)) > len("[bug] ")+maxTitleRunes {
		t.Fatalf("title = %q, want it truncated", title)
	}
}

func TestReportTitle_UsesFirstLineOnly(t *testing.T) {
	report, err := NewReport(reporter(), KindConfusing, "what is re-acquire?\nI tapped it twice", Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if report.Title() != "[confusing] what is re-acquire?" {
		t.Fatalf("title = %q, want only the first line", report.Title())
	}
}

// TestReportTitle_DropsInvisibleRunes reproduces #1107: a directional override
// or zero-width rune in the first line survived into the title, so the rendered
// title could be visually spoofed.
func TestReportTitle_DropsInvisibleRunes(t *testing.T) {
	message := "\u202Etxt.exe\u200B  play\u2066back\u00AD stops\u200F\nmore detail"
	report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	title := report.Title()
	for _, r := range title {
		if isInvisible(r) {
			t.Fatalf("title = %q, still carries invisible rune %U", title, r)
		}
	}
	if want := "[bug] txt.exe playback stops"; title != want {
		t.Fatalf("title = %q, want %q", title, want)
	}
}

// TestReportTitle_DropsInvisibleRunesWithoutANewline covers a single-line
// message, whose first line skips the newline-cut branch.
func TestReportTitle_DropsInvisibleRunesWithoutANewline(t *testing.T) {
	report, err := NewReport(reporter(), KindIdea, "\u202Esort albums by year\u200B", Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if want := "[idea] sort albums by year"; report.Title() != want {
		t.Fatalf("title = %q, want %q", report.Title(), want)
	}
}

// TestReportTitle_SkipsInvisibleOnlyLeadingLines reproduces #1108: a message
// valid overall but whose first line holds only invisible runes produced the
// blank title "[bug] ".
func TestReportTitle_SkipsInvisibleOnlyLeadingLines(t *testing.T) {
	cases := map[string]string{
		"zero-width first line":     zeroWidthSpace + "  \nDetails that explain the bug in enough length",
		"filler first line":         "\u3164\u1160\n\nDetails that explain the bug in enough length",
		"several blank lines":       "\u200B\n\u115F \u2800\n \nDetails that explain the bug in enough length",
		"crlf invisible first line": "\u200B\r\nDetails that explain the bug in enough length",
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
			if err != nil {
				t.Fatalf("NewReport: %v", err)
			}
			if want := "[bug] Details that explain the bug in enough length"; report.Title() != want {
				t.Fatalf("title = %q, want %q", report.Title(), want)
			}
		})
	}
}

// TestTruncate_NeverSplitsAGraphemeCluster reproduces #1109: truncate cut by
// rune count, so a ZWJ emoji sequence, flag, skin-tone emoji, or combining-mark
// sequence straddling the cut left a dangling fragment before the ellipsis.
func TestTruncate_NeverSplitsAGraphemeCluster(t *testing.T) {
	cases := []struct {
		name  string
		s     string
		limit int
		want  string
	}{
		{"cut before a zwj", "abc\U0001F468\u200D\U0001F469\u200D\U0001F467xyz", 5, "abc…"},
		{"cut after a zwj", "abc\U0001F468\u200D\U0001F469\u200D\U0001F467xyz", 6, "abc…"},
		{"cut inside a zwj sequence", "abc\U0001F468\u200D\U0001F469\u200D\U0001F467xyz", 8, "abc…"},
		{"whole zwj sequence kept", "abc\U0001F468\u200D\U0001F469\u200D\U0001F467xyz", 9, "abc\U0001F468\u200D\U0001F469\u200D\U0001F467…"},
		{"cut inside a flag", "ab\U0001F1EE\U0001F1F9\U0001F1EB\U0001F1F7cd", 4, "ab…"},
		{"cut between two flags", "ab\U0001F1EE\U0001F1F9\U0001F1EB\U0001F1F7cd", 5, "ab\U0001F1EE\U0001F1F9…"},
		{"cut inside the second flag", "ab\U0001F1EE\U0001F1F9\U0001F1EB\U0001F1F7cd", 6, "ab\U0001F1EE\U0001F1F9…"},
		{"cut before a combining mark", "cafe\u0301 au lait", 5, "caf…"},
		{"cut between stacked combining marks", "xo\u0323\u0302 and more", 4, "x…"},
		{"cut before a skin-tone modifier", "hi\U0001F44D\U0001F3FDthere", 4, "hi…"},
		{"cut before a variation selector", "ok\u2764\uFE0F more text", 4, "ok…"},
		{"cut before a spacing mark", "\u0915\u093F\u0915\u093F\u0915\u093F", 4, "\u0915\u093F…"},
		{"cut inside hangul jamo", "a\u1100\u1161\u11A8b", 3, "a…"},
		{"cut between an lv syllable and a trailing jamo", "a\uAC00\u11A8b", 3, "a…"},
		{"cut between an lvt syllable and a trailing jamo", "a\uAC01\u11A8b", 3, "a…"},
		{"cut between an lvt syllable and a vowel jamo", "a\uAC01\u1161b", 3, "a\uAC01…"},
		{"cluster longer than the limit", "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466 hi", 4, "…"},
		{"cut after a prepended mark", "ab\u0600\u0661\u0662", 4, "ab…"},
		{"cut inside a thai sara am", "ab\u0E01\u0E33cd", 4, "ab…"},
		{"cut inside a crlf", "ab\r\ncd", 4, "ab…"},
		{"plain text still cut at the limit", "abcdefgh", 5, "abcd…"},
		{"short text untouched", "\U0001F468\u200D\U0001F469", 3, "\U0001F468\u200D\U0001F469"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.s, tc.limit)
			if got != tc.want {
				t.Fatalf("truncate(%+q, %d) = %+q, want %+q", tc.s, tc.limit, got, tc.want)
			}
			if n := len([]rune(got)); n > tc.limit {
				t.Fatalf("truncate(%+q, %d) = %+q is %d runes, over the limit", tc.s, tc.limit, got, n)
			}
		})
	}
}

// TestTruncate_NonPositiveLimitReturnsEmpty reproduces #1109: a limit <= 0
// sliced with a negative bound and panicked.
func TestTruncate_NonPositiveLimitReturnsEmpty(t *testing.T) {
	for _, limit := range []int{0, -1, math.MinInt} {
		for _, s := range []string{"", "a", "some longer text"} {
			if got := truncate(s, limit); got != "" {
				t.Fatalf("truncate(%q, %d) = %q, want \"\"", s, limit, got)
			}
		}
	}
}

func TestFeedbackValidationErrorCode(t *testing.T) {
	if got := NewValidationError("x").ErrorCode(); got != "feedback.validation_error" {
		t.Errorf("code: got %q, want %q", got, "feedback.validation_error")
	}
}
