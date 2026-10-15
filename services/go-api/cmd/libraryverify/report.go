package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

type verifyReport struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Partial     bool            `json:"partial,omitempty"`
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
	if err := encodeJSON(out, report); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func encodeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func saveReport(path string, report verifyReport) error {
	return saveJSON(path, report)
}

func saveJSON(path string, value any) error {
	pending := path + ".pending"
	if err := writePending(pending, value); err != nil {
		_ = os.Remove(pending)
		return err
	}
	if err := os.Rename(pending, path); err != nil {
		_ = os.Remove(pending)
		return fmt.Errorf("publish report: %w", err)
	}
	return nil
}

func writePending(pending string, value any) error {
	file, err := os.Create(pending)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := encodeJSON(file, value); err != nil {
		_ = file.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
}

func checkpointTo(path string, minScore float64, progress io.Writer) func([]trackResult) {
	return func(finished []trackResult) {
		partial := buildReport(finished, minScore, time.Now())
		partial.Partial = true
		if err := saveReport(path, partial); err != nil {
			_, _ = fmt.Fprintf(progress, "libraryverify: checkpoint of %d tracks failed: %v\n", len(finished), err)
		}
	}
}

var summaryOrder = []verdict{
	verdictConfirmed, verdictConfirmedPreview, verdictWrongAudio, verdictMismatch, verdictUnknown, verdictUnofficial, verdictUnbound, verdictUnverifiable, verdictFailed,
}

var listedVerdicts = []verdict{verdictMismatch, verdictWrongAudio, verdictFailed}

func writeSummary(out io.Writer, report verifyReport, reportPath string) {
	_, _ = fmt.Fprintf(out, "verified %d tracks at AcoustID score >= %.2f\n", report.Total, report.MinScore)
	for _, v := range summaryOrder {
		_, _ = fmt.Fprintf(out, "  %-13s %d\n", v+":", report.Counts[v])
	}
	for _, result := range report.Results {
		if slices.Contains(listedVerdicts, result.Verdict) {
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
		line += fmt.Sprintf("\n  bound %s, audio says %s %q %v (%.2f)", result.Identity.MBRecordingID, top.MBRecordingID, top.Title, top.Artists, top.Score)
	}
	if preview := result.Preview; preview != nil && preview.Alignment != nil {
		line += fmt.Sprintf("\n  preview of %s: bit error rate %.3f at %.1fs", result.Identity.ISRC, preview.Alignment.BitErrorRate, preview.Alignment.OffsetSeconds)
	}
	return line
}
