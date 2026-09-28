package providers

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"strings"
	"time"
)

const reporterIdentityRow = "Reporter"

func renderBody(report *domain.Report, correlationID string) string {
	diag := literalDiagnostics(report.Diagnostics)
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

func literalDiagnostics(d domain.Diagnostics) domain.Diagnostics {
	return domain.NewDiagnostics(inlineCode(d.AppVersion), inlineCode(d.Platform), inlineCode(d.OSVersion), inlineCode(d.Screen))
}

func inlineCode(text string) string {
	if text == "" {
		return ""
	}
	delim := strings.Repeat("`", longestBacktickRun(text)+1)
	if strings.ContainsAny(text[:1]+text[len(text)-1:], "` ") {
		text = " " + text + " "
	}
	return delim + text + delim
}

const fullwidthAt = "\uFF20"

func plainTitle(title string) string {
	return strings.ReplaceAll(title, "@", fullwidthAt)
}
