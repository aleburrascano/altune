package domain

import (
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

func TestParseKind_RejectsUnknown(t *testing.T) {
	if _, err := ParseKind("rant"); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestNewReport_RejectsTooShortMessage(t *testing.T) {
	if _, err := NewReport(reporter(), KindBug, "broken", Diagnostics{}); err == nil {
		t.Fatal("expected a too-short message to be rejected")
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
