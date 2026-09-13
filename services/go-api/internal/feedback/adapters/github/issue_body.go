package github

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"strings"
	"time"
)

func renderBody(report *domain.Report) string {
	diag := report.Diagnostics
	rows := [][2]string{
		{"Reporter", report.Reporter.String()},
		{"App", diag.AppVersion},
		{"Platform", strings.TrimSpace(diag.Platform + " " + diag.OSVersion)},
		{"Screen", diag.Screen},
		{"Reported", report.SubmittedAt.Format(time.RFC3339)},
	}
	var b strings.Builder
	b.WriteString(report.Message)
	b.WriteString("\n\n---\n\n| | |\n| --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", row[0], cell(row[1]))
	}
	return b.String()
}

func cell(value string) string {
	if value == "" {
		return "—"
	}
	return strings.ReplaceAll(value, "|", `\|`)
}
