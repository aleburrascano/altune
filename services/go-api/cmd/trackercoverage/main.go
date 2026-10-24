package main

import (
	"altune/go-api/internal/discovery/adapters/providers/trackers"
	"altune/go-api/internal/discovery/domain"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

const defaultHubSheetID = "1Z8aANbxXbnUGoZPRvJfWL3gz6jrzPPrwVt3d0c1iJ_4"

type artistCoverage struct {
	name  string
	songs int
	err   error
}

type songLister interface {
	Artists(ctx context.Context) ([]string, error)
	ArtistSongs(ctx context.Context, artistName string) ([]domain.UnreleasedSong, error)
}

func main() {
	hub := flag.String("hub", defaultHubSheetID, "TrackerHub spreadsheet id")
	pause := flag.Duration("pause", 2200*time.Millisecond, "wait between artists, to stay under the Sheets per-user quota")
	limit := flag.Int("limit", 0, "check only the first N artists (0 = all)")
	flag.Parse()
	key := os.Getenv("GOOGLE_SHEETS_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "GOOGLE_SHEETS_API_KEY is not set")
		os.Exit(2)
	}
	client := trackers.NewClient(http.DefaultClient, key, *hub, &memoryIndex{})
	if err := run(context.Background(), client, os.Stdout, *pause, *limit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type memoryIndex struct {
	index map[string]string
}

func (m *memoryIndex) Get(context.Context) (map[string]string, bool) {
	return m.index, m.index != nil
}

func (m *memoryIndex) Set(_ context.Context, index map[string]string, _ time.Duration) error {
	m.index = index
	return nil
}

func run(ctx context.Context, client songLister, out io.Writer, pause time.Duration, limit int) error {
	names, err := client.Artists(ctx)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(names) {
		names = names[:limit]
	}
	rows := make([]artistCoverage, 0, len(names))
	for i, name := range names {
		if i > 0 {
			time.Sleep(pause)
		}
		songs, err := client.ArtistSongs(ctx, name)
		rows = append(rows, artistCoverage{name: name, songs: len(songs), err: err})
	}
	report(out, rows)
	return nil
}

func report(out io.Writer, rows []artistCoverage) {
	writeSummary(out, rows)
	slices.SortFunc(rows, bySongsThenName)
	for _, r := range rows {
		status := fmt.Sprint(r.songs)
		if r.err != nil {
			status = "error: " + r.err.Error()
		}
		_, _ = fmt.Fprintf(out, "%s\t%s\n", r.name, status)
	}
}

func writeSummary(out io.Writer, rows []artistCoverage) {
	withSongs, failed, total := 0, 0, 0
	for _, r := range rows {
		total += r.songs
		switch {
		case r.err != nil:
			failed++
		case r.songs > 0:
			withSongs++
		}
	}
	_, _ = fmt.Fprintf(out, "artists %d, with songs %d, empty %d, errors %d, songs %d\n",
		len(rows), withSongs, len(rows)-withSongs-failed, failed, total)
}

func bySongsThenName(a, b artistCoverage) int {
	if a.songs != b.songs {
		return b.songs - a.songs
	}
	return strings.Compare(a.name, b.name)
}
