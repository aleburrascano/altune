package domain

import (
	"strings"
	"testing"
)

var githubToken = "ghp_" + strings.Repeat("Ab3", 12)

func splitByZeroWidthSpace(token string) string {
	return token[:8] + zeroWidthSpace + token[8:]
}

func TestNewReport_RedactsMessageTokenSplitByInvisibleRunes(t *testing.T) {
	family := "\U0001F468" + zeroWidthJoiner + "\U0001F469" + zeroWidthJoiner + "\U0001F467"
	message := "sync fails " + family + "\nlog: " + splitByZeroWidthSpace(githubToken) + " end\nthird line"
	report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	want := "sync fails " + family + "\nlog: " + RedactedMarker + " end\nthird line"
	if report.Message != want {
		t.Fatalf("message = %q, want %q", report.Message, want)
	}
}

func TestReportTitle_RedactsTokenRejoinedByInvisibleStrip(t *testing.T) {
	report := &Report{Kind: KindBug, Message: "log " + splitByZeroWidthSpace(githubToken) + " end"}
	if want := "[bug] log " + RedactedMarker + " end"; report.Title() != want {
		t.Fatalf("title = %q, want %q", report.Title(), want)
	}
}

func TestNewReport_RedactsProviderTokenGluedByInvisibleRune(t *testing.T) {
	cases := map[string]string{
		"github":   githubToken,
		"aws":      "AKIA" + strings.Repeat("Q7", 8),
		"npm":      "npm_" + strings.Repeat("a1B2", 9),
		"gitlab":   "glpat-" + strings.Repeat("x9Y", 7),
		"slack":    "xoxb-" + strings.Repeat("12ab", 4),
		"google":   "AIza" + strings.Repeat("k", 35),
		"stripe":   "sk_live_" + strings.Repeat("z4", 8),
		"sendgrid": "SG." + strings.Repeat("k", 22) + "." + strings.Repeat("v", 43),
	}
	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			assertSecretRedacted(t, "x"+zeroWidthSpace, secret)
		})
	}
}

func TestNewReport_RedactsNpmTokenLongerThanThirtySix(t *testing.T) {
	assertSecretRedacted(t, "", "npm_"+strings.Repeat("a1B2", 10))
}

func TestNewReport_LeavesHyphenatedProseEndingInSkUnredacted(t *testing.T) {
	message := "my brisk-walking-playlist-mixes screen froze"
	report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if report.Message != message {
		t.Fatalf("message = %q, want it unchanged", report.Message)
	}
}
