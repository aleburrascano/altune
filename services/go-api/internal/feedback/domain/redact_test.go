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
