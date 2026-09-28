package main

import (
	"altune/go-api/internal/acquisition/service/eval"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"time"
)

const (
	logCandidateEvaluatedMsg = "candidate_evaluated"
	logRejectionSummaryMsg   = "acquisition.rejection_summary"
)

type captureTrackDump struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	Album           string  `json:"album"`
	DurationSeconds float64 `json:"duration_seconds"`
	ISRC            string  `json:"isrc"`
}

type captureDump struct {
	Track captureTrackDump  `json:"track"`
	Logs  []json.RawMessage `json:"logs"`
}

type captureLogLine struct {
	Msg               string  `json:"msg"`
	Source            string  `json:"source"`
	CandidateTitle    string  `json:"candidate_title"`
	CandidateChannel  string  `json:"candidate_channel"`
	CandidateDuration float64 `json:"candidate_duration"`
	CandidateViews    int64   `json:"candidate_views"`
	Summary           string  `json:"summary"`
}

func runCapture(args []string, stdin io.Reader, stdout io.Writer, now time.Time) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tier := fs.String("tier", "unknown", "tier the dump was captured from (staging|prod)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: capture: %v\n", err)
		return 2
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: capture: read dump: %v\n", err)
		return 2
	}

	kase, err := captureCase(raw, *tier, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: capture: %v\n", err)
		return 2
	}

	out, err := json.MarshalIndent(kase, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: capture: %v\n", err)
		return 2
	}
	if _, err := fmt.Fprintln(stdout, string(out)); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: capture: write output: %v\n", err)
		return 2
	}
	return 0
}

func captureCase(raw []byte, tier string, now time.Time) (eval.Case, error) {
	var d captureDump
	if err := json.Unmarshal(raw, &d); err != nil {
		return eval.Case{}, fmt.Errorf("parse dump: %w", err)
	}
	if strings.TrimSpace(d.Track.Title) == "" || strings.TrimSpace(d.Track.Artist) == "" {
		return eval.Case{}, fmt.Errorf("dump has no track title/artist")
	}

	sources, rejectionSummary, err := captureSources(d.Logs)
	if err != nil {
		return eval.Case{}, err
	}
	if len(sources) == 0 {
		return eval.Case{}, fmt.Errorf("no candidate_evaluated log lines in dump")
	}

	return eval.Case{
		ID:    captureID(d.Track),
		Class: "RW",
		Note:  captureNote(rejectionSummary, tier, now),
		Track: eval.Track{
			Title:    d.Track.Title,
			Artist:   d.Track.Artist,
			Album:    d.Track.Album,
			Duration: d.Track.DurationSeconds,
			ISRC:     d.Track.ISRC,
		},
		Sources: sources,
	}, nil
}

func captureSources(logs []json.RawMessage) ([]eval.Source, string, error) {
	var sources []eval.Source
	index := map[string]int{}
	counter := map[string]int{}
	var rejectionSummary string

	for _, raw := range logs {
		var line captureLogLine
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, "", fmt.Errorf("parse log line: %w", err)
		}
		switch line.Msg {
		case logCandidateEvaluatedMsg:
			addCapturedCandidate(&sources, index, counter, line)
		case logRejectionSummaryMsg:
			if line.Summary != "" {
				rejectionSummary = line.Summary
			}
		}
	}
	return sources, rejectionSummary, nil
}

func addCapturedCandidate(sources *[]eval.Source, index, counter map[string]int, line captureLogLine) {
	name := line.Source
	if name == "" {
		name = "unknown"
	}
	i, ok := index[name]
	if !ok {
		*sources = append(*sources, eval.Source{Name: name})
		i = len(*sources) - 1
		index[name] = i
	}
	counter[name]++
	(*sources)[i].Candidates = append((*sources)[i].Candidates, eval.Candidate{
		Title:     line.CandidateTitle,
		URL:       fmt.Sprintf("captured:%s:%d", name, counter[name]),
		Channel:   line.CandidateChannel,
		Duration:  line.CandidateDuration,
		ViewCount: line.CandidateViews,
	})
}

func captureNote(rejectionSummary, tier string, now time.Time) string {
	note := fmt.Sprintf("captured %s from %s; review before committing", now.Format("2006-01-02"), tier)
	if rejectionSummary == "" {
		return note
	}
	return rejectionSummary + "; " + note
}

func captureID(t captureTrackDump) string {
	if id := strings.TrimSpace(t.ID); id != "" {
		return "captured-" + id
	}
	if s := slug(t.Title + "-" + t.Artist); s != "" {
		return "captured-" + s
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Title + "\x00" + t.Artist))
	return fmt.Sprintf("captured-%08x", h.Sum32())
}

func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
