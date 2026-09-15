package providers

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"strings"
	"time"
)

func renderBody(report *domain.Report, correlationID string) string {
	diag := literalDiagnostics(report.Diagnostics)
	rows := [][2]string{
		{"Reporter", report.Reporter.String()},
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

// literalDiagnostics wraps each client-supplied diagnostics value in an inline
// code span, extending fenced's guarantee to the table: no @mention notifies
// and no link, image, or HTML renders. The domain already flattened each value
// to one line, so a span cannot be broken by a newline.
func literalDiagnostics(d domain.Diagnostics) domain.Diagnostics {
	return domain.NewDiagnostics(inlineCode(d.AppVersion), inlineCode(d.Platform), inlineCode(d.OSVersion), inlineCode(d.Screen))
}

// inlineCode renders single-line untrusted text as a code span whose delimiter
// outlasts any backtick run inside it. Text starting or ending with a backtick
// or space is padded by one space on each side, which the span strips again.
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

// fullwidthAt stands in for "@" in an issue title: it reads the same, but
// GitHub never treats it as the start of a mention.
const fullwidthAt = "\uFF20"

// plainTitle neutralizes @mentions in an issue title. GitHub matches mentions
// in titles too and titles cannot be fenced, so the one app-wide token would
// otherwise notify any account or team a reporter names.
func plainTitle(title string) string {
	return strings.ReplaceAll(title, "@", fullwidthAt)
}
