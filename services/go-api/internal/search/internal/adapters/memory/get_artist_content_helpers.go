package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"log/slog"
)

func AlbumTitles(items []domain.SearchResult) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func CaptureProductionLogs(t TB) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	t.Cleanup(logging.Swap(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))))
	return &buf
}
