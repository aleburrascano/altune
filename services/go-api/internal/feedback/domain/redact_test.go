package domain

import (
	"strings"
	"testing"
)

func assertSecretRedacted(t *testing.T, keep, secret string) {
	t.Helper()
	message := "playback broke, log says " + keep + secret + " then it crashed"
	report, err := NewReport(reporter(), KindBug, message, Diagnostics{})
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	want := "playback broke, log says " + keep + RedactedMarker + " then it crashed"
	if report.Message != want {
		t.Fatalf("message = %q, want %q", report.Message, want)
	}
}

func TestNewReport_RedactsPrefixedSecretLabels(t *testing.T) {
	cases := map[string]string{
		"prefixed password":   "DB_PASSWORD=",
		"prefixed secret":     "STRIPE_SECRET=",
		"prefixed api key":    "OPENAI_API_KEY=",
		"secret with suffix":  "aws_secret_access_key=",
		"prefixed token":      "GITHUB_TOKEN: ",
		"quoted prefixed key": `"SERVICE_API_KEY": "`,
		"header api key":      "x-api-key: ",
	}
	for name, keep := range cases {
		t.Run(name, func(t *testing.T) {
			assertSecretRedacted(t, keep, "hunter2hunter2")
		})
	}
}

func TestNewReport_RedactsProviderKeysAndTokenAssignments(t *testing.T) {
	cases := map[string]struct{ secret, keep string }{
		"openai key":           {"sk-" + strings.Repeat("Ab1", 16), ""},
		"openai project key":   {"sk-" + "proj-" + strings.Repeat("Ab1_", 12), ""},
		"anthropic key":        {"sk-" + "ant-" + "api03-" + strings.Repeat("Ab1-", 20), ""},
		"npm token":            {"npm_" + strings.Repeat("a1B2", 9), ""},
		"gitlab token":         {"glpat-" + strings.Repeat("x9Y", 7), ""},
		"sendgrid key":         {"SG." + strings.Repeat("k", 22) + "." + strings.Repeat("v", 43), ""},
		"authorization token":  {strings.Repeat("f0e1", 10), "Authorization: token "},
		"bare token assign":    {strings.Repeat("q7", 10), "token="},
		"spaced token assign":  {strings.Repeat("q7", 10), "token = "},
		"query token argument": {strings.Repeat("q7", 10), "/callback?token="},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assertSecretRedacted(t, tc.keep, tc.secret)
		})
	}
}

func TestNewReport_LeavesProseNearSecretWordsUnredacted(t *testing.T) {
	for _, message := range []string{
		"my secretary: she says the app froze",
		"the playlist is password-protected: I cannot open it",
		"invalid token: please sign in again, it says",
		"the task-list screen shows sk-8 as a track name",
		"tokenizer: the search splits words oddly",
		"SG.1 firmware on my speaker drops the stream",
		"npm_ install failed while building the app",
		"authorization: denied when opening my library",
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
