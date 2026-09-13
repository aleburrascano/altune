package domain

import (
	"errors"
	"strings"
	"testing"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func reporter() shared.UserId { return shared.NewUserId(uuid.New()) }

func TestParseKind_MapsToGitHubLabels(t *testing.T) {
	cases := map[string]string{"bug": "bug", "idea": "enhancement", "confusing": "ux"}
	for input, label := range cases {
		kind, err := ParseKind(input)
		if err != nil {
			t.Fatalf("ParseKind(%q): %v", input, err)
		}
		if kind.Label() != label {
			t.Fatalf("ParseKind(%q).Label() = %q, want %q", input, kind.Label(), label)
		}
	}
}

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
		if kind.Label() != "" {
			t.Fatalf("Kind(%d).Label() = %q, want empty", int(kind), kind.Label())
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

func TestFeedbackValidationErrorCode(t *testing.T) {
	if got := NewValidationError("x").ErrorCode(); got != "feedback.validation_error" {
		t.Errorf("code: got %q, want %q", got, "feedback.validation_error")
	}
}
