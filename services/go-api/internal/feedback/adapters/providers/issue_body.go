package providers

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"strings"
	"time"
)

// reporterIdentityRow labels the diagnostics row that publishes the reporter's
// opaque shared.UserId UUID into every filed issue. Publishing it is deliberate,
// even though the issue repo is public: it is the durable locator an operator
// uses to find and delete a user's reports on an erasure request, since a filed
// issue is never read back, redacted, or removed by the service
// (internal/feedback/RETENTION.md). No email, handle, or IP is ever published.
// .env.example names this constant so the shipped contract cannot drift again.
const reporterIdentityRow = "Reporter"

func renderBody(report *domain.Report, correlationID string) string {
	diag := report.Diagnostics
	rows := [][2]string{
		{reporterIdentityRow, report.Reporter.String()},
		{"App", diag.AppVersion},
		{"Platform", strings.TrimSpace(diag.Platform + " " + diag.OSVersion)},
		{"Screen", diag.Screen},
		{"Reported", report.SubmittedAt.Format(time.RFC3339)},
		{"Correlation ID", correlationID},
	}
	var b strings.Builder
	b.WriteString(fenced(report.Message))
	b.WriteString("\n\n---\n\n| | |\n| --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", row[0], cell(row[1]))
	}
	return b.String()
}

// fenced renders untrusted text inside a code block so GitHub shows it
// literally: no @mentions notify, no links or images render, and no forged
// table can masquerade as the diagnostics. The fence is longer than any
// backtick run in the text, so the text cannot close it early.
func fenced(text string) string {
	fence := strings.Repeat("`", max(3, longestBacktickRun(text)+1))
	return fence + "\n" + text + "\n" + fence
}

func longestBacktickRun(text string) int {
	longest, run := 0, 0
	for _, r := range text {
		if r != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	return longest
}

func cell(value string) string {
	if value == "" {
		return "—"
	}
	return strings.ReplaceAll(value, "|", `\|`)
}
