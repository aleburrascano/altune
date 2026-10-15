package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type verifyReport struct {
	GeneratedAt time.Time       `json:"generated_at"`
	MinScore    float64         `json:"min_score"`
	Total       int             `json:"total"`
	Counts      map[verdict]int `json:"counts"`
	Results     []trackResult   `json:"results"`
}

func buildReport(results []trackResult, minScore float64, now time.Time) verifyReport {
	return verifyReport{GeneratedAt: now.UTC(), MinScore: minScore, Total: len(results), Counts: countVerdicts(results), Results: results}
}

func countVerdicts(results []trackResult) map[verdict]int {
	counts := map[verdict]int{}
	for _, result := range results {
		counts[result.Verdict]++
	}
	return counts
}

func writeReport(out io.Writer, report verifyReport) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func saveReport(path string, report verifyReport) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := writeReport(file, report); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
}

var summaryOrder = []verdict{
	verdictConfirmed, verdictMismatch, verdictUnknown, verdictUnofficial, verdictUnbound, verdictUnverifiable, verdictFailed,
}

func writeSummary(out io.Writer, report verifyReport, reportPath string) {
	_, _ = fmt.Fprintf(out, "verified %d tracks at AcoustID score >= %.2f\n", report.Total, report.MinScore)
	for _, v := range summaryOrder {
		_, _ = fmt.Fprintf(out, "  %-13s %d\n", v+":", report.Counts[v])
	}
	for _, result := range report.Results {
		if result.Verdict == verdictMismatch || result.Verdict == verdictFailed {
			_, _ = fmt.Fprintln(out, describe(result))
		}
	}
	_, _ = fmt.Fprintf(out, "report: %s\n", reportPath)
}

func describe(result trackResult) string {
	line := fmt.Sprintf("%-8s track %s %s - %s", result.Verdict, result.TrackID, result.Artist, result.Title)
	if result.Error != "" {
		return line + "\n  error: " + result.Error
	}
	if len(result.Candidates) > 0 {
		top := result.Candidates[0]
		line += fmt.Sprintf("\n  bound %s, audio says %s %v (%.2f)", result.Identity.MBRecordingID, top.MBRecordingID, top.Artists, top.Score)
	}
	return line
}
