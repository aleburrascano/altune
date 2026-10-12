package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	liveIntervalEnv      = "ACQEVAL_LIVE_INTERVAL"
	liveTimeoutEnv       = "ACQEVAL_LIVE_TIMEOUT"
	defaultLiveInterval  = time.Second
	defaultLiveTimeout   = 20 * time.Second
	liveProgressEvery    = 25
	liveMaxLineBytes     = 4 << 20
	liveUsageExitCode    = 2
	durationDeltaSummary = "duration_delta"
)

var errLiveFlagsRequired = errors.New("-tracks and -out are required")

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

var youtubeHosts = map[string]bool{
	"youtube.com":       true,
	"www.youtube.com":   true,
	"m.youtube.com":     true,
	"music.youtube.com": true,
	"youtu.be":          true,
}

type liveEvidence struct {
	DurationDeltaSeconds float64 `json:"duration_delta_seconds"`
	SourceTitle          string  `json:"source_title"`
}

type liveTrack struct {
	ID                    string        `json:"id"`
	Title                 string        `json:"title"`
	Artist                string        `json:"artist"`
	Album                 string        `json:"album"`
	DurationSeconds       float64       `json:"duration_seconds"`
	AudioSourceURL        string        `json:"audio_source_url"`
	AcquisitionProvenance string        `json:"acquisition_provenance"`
	Evidence              *liveEvidence `json:"acquisition_evidence"`
}

type judgedRow struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Artist      string   `json:"artist"`
	URL         string   `json:"url"`
	SourceTitle string   `json:"source_title"`
	Author      string   `json:"author"`
	VideoType   string   `json:"video_type"`
	Flags       []string `json:"flags"`
	Marker      string   `json:"marker,omitempty"`
}

func (t liveTrack) durationDeltaSeconds() float64 {
	if t.Evidence == nil {
		return 0
	}
	return t.Evidence.DurationDeltaSeconds
}

func (t liveTrack) evidenceSourceTitle() string {
	if t.Evidence == nil {
		return ""
	}
	return t.Evidence.SourceTitle
}

func liveConfigFromEnv(getenv func(string) string) (ytmsearch.Config, error) {
	interval, err := durationFromEnv(getenv, liveIntervalEnv, defaultLiveInterval)
	if err != nil {
		return ytmsearch.Config{}, err
	}
	timeout, err := durationFromEnv(getenv, liveTimeoutEnv, defaultLiveTimeout)
	if err != nil {
		return ytmsearch.Config{}, err
	}
	return ytmsearch.Config{MinInterval: interval, Timeout: timeout}, nil
}

func durationFromEnv(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	raw := getenv(name)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func runLiveCommand(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := liveConfigFromEnv(getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: live: %v\n", err)
		return liveUsageExitCode
	}
	return runLive(args, stdout, ytmsearch.New(cfg))
}

func runLive(args []string, stdout io.Writer, client *ytmsearch.Client) int {
	if err := executeLive(args, stdout, client); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: live: %v\n", err)
		return liveUsageExitCode
	}
	return 0
}

func executeLive(args []string, stdout io.Writer, client *ytmsearch.Client) error {
	tracksPath, outPath, err := parseLiveArgs(args)
	if err != nil {
		return err
	}
	tracks, err := readLiveTracks(tracksPath)
	if err != nil {
		return err
	}
	rows, err := judgeToFile(context.Background(), client, tracks, outPath)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, renderLiveSummary(rows))
	return err
}

func parseLiveArgs(args []string) (tracksPath, outPath string, err error) {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tracks := fs.String("tracks", "", "jsonl of stored tracks (acq-debug.sh export-tracks)")
	out := fs.String("out", "", "jsonl file to write one judged row per track")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if *tracks == "" || *out == "" {
		return "", "", errLiveFlagsRequired
	}
	return *tracks, *out, nil
}

func readLiveTracks(path string) ([]liveTrack, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tracks: %w", err)
	}
	defer func() { _ = file.Close() }()
	return decodeLiveTracks(file)
}

func decodeLiveTracks(r io.Reader) ([]liveTrack, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), liveMaxLineBytes)
	var tracks []liveTrack
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var track liveTrack
		if err := json.Unmarshal([]byte(line), &track); err != nil {
			return nil, fmt.Errorf("tracks line %d: %w", lineNumber, err)
		}
		tracks = append(tracks, track)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read tracks: %w", err)
	}
	return tracks, nil
}

func judgeToFile(ctx context.Context, client *ytmsearch.Client, tracks []liveTrack, outPath string) ([]judgedRow, error) {
	out, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create out: %w", err)
	}
	defer func() { _ = out.Close() }()

	encoder := json.NewEncoder(out)
	rows := make([]judgedRow, 0, len(tracks))
	for i, track := range tracks {
		row := judgeTrack(ctx, client, track)
		if err := encoder.Encode(row); err != nil {
			return nil, fmt.Errorf("write out: %w", err)
		}
		rows = append(rows, row)
		if (i+1)%liveProgressEvery == 0 {
			fmt.Fprintf(os.Stderr, "acquisitioneval: live: judged %d of %d\n", i+1, len(tracks))
		}
	}
	return rows, nil
}

func judgeTrack(ctx context.Context, client *ytmsearch.Client, track liveTrack) judgedRow {
	row := judgedRow{ID: track.ID, Title: track.Title, Artist: track.Artist, URL: track.AudioSourceURL, Flags: []string{}}
	host := sourceHost(track.AudioSourceURL)
	if !youtubeHosts[host] {
		row.Marker = "skipped:" + host
		return row
	}
	videoID, ok := youtubeVideoID(track.AudioSourceURL)
	if !ok {
		row.Flags = append(row.Flags, "lookup_failed:video_id")
		return row
	}
	src, failed := lookUpYouTube(ctx, client, track, videoID)
	row.SourceTitle, row.Author, row.VideoType = src.Title, src.Author, string(src.Type)
	row.Flags = append(youtubeFlags(track, src), failed...)
	return row
}

func sourceHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func lookUpYouTube(ctx context.Context, client *ytmsearch.Client, track liveTrack, videoID string) (youtubeSource, []string) {
	var failed []string
	watch, err := client.Watch(ctx, videoID)
	if err != nil {
		failed = append(failed, "lookup_failed:next")
	}
	oembed, err := client.OEmbed(ctx, videoID)
	if err != nil {
		failed = append(failed, "lookup_failed:oembed")
	}
	songs, err := client.SearchSongs(ctx, track.Title+" "+track.Artist)
	if err != nil {
		failed = append(failed, "lookup_failed:search")
	}
	title := firstNonEmpty(oembed.Title, watch.Title, track.evidenceSourceTitle())
	return youtubeSource{VideoID: videoID, Title: title, Author: oembed.Author, Type: watch.Type, Songs: songs}, failed
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func youtubeVideoID(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !youtubeHosts[strings.ToLower(parsed.Hostname())] {
		return "", false
	}
	candidate := parsed.Query().Get("v")
	if strings.EqualFold(parsed.Hostname(), "youtu.be") {
		candidate = strings.TrimPrefix(parsed.Path, "/")
	}
	if !videoIDPattern.MatchString(candidate) {
		return "", false
	}
	return candidate, true
}

func summaryKey(flagName string) string {
	if strings.HasPrefix(flagName, durationDeltaSummary+":") {
		return durationDeltaSummary
	}
	return flagName
}

func countFlags(rows []judgedRow) (counts map[string]int, flagged int) {
	counts = map[string]int{}
	for _, row := range rows {
		if len(row.Flags) > 0 {
			flagged++
		}
		for _, flagName := range row.Flags {
			counts[summaryKey(flagName)]++
		}
		if row.Marker != "" {
			counts[row.Marker]++
		}
	}
	return counts, flagged
}

func byCountThenName(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

func renderLiveSummary(rows []judgedRow) string {
	counts, flagged := countFlags(rows)
	var b strings.Builder
	for _, key := range byCountThenName(counts) {
		fmt.Fprintf(&b, "%-36s %d\n", key, counts[key])
	}
	fmt.Fprintf(&b, "flagged %d of %d\n", flagged, len(rows))
	return b.String()
}
