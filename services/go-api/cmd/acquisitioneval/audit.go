package main

import (
	"altune/go-api/internal/acquisition/service"
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const auditFieldCount = 5

type auditRow struct {
	trackID     string
	title       string
	artist      string
	sourceTitle string
}

func parseAuditRow(line string) (auditRow, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != auditFieldCount {
		return auditRow{}, fmt.Errorf("want %d tab-separated fields, got %d", auditFieldCount, len(fields))
	}
	return auditRow{trackID: fields[0], title: fields[1], artist: fields[2], sourceTitle: fields[4]}, nil
}

func (r auditRow) vetoWords() []string {
	if r.sourceTitle == "" {
		return nil
	}
	veto, _ := service.UnrequestedQualifiers(r.title, r.artist, r.sourceTitle)
	return veto
}

func (r auditRow) flaggedLine(words []string) string {
	return strings.Join([]string{r.trackID, r.title, r.artist, r.sourceTitle, strings.Join(words, ",")}, "\t")
}

func auditQualifiers(stdin io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(stdin)
	total, flagged := 0, 0
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		row, err := parseAuditRow(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", total+1, err)
		}
		total++
		if words := row.vetoWords(); len(words) > 0 {
			flagged++
			fmt.Fprintln(stdout, row.flaggedLine(words))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read tsv: %w", err)
	}
	fmt.Fprintf(stdout, "flagged %d of %d\n", flagged, total)
	return nil
}

func runAuditQualifiers(stdin io.Reader, stdout io.Writer) int {
	if err := auditQualifiers(stdin, stdout); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: audit-qualifiers: %v\n", err)
		return 2
	}
	return 0
}
