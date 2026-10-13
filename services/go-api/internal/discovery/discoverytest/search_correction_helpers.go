package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

type QueryFakeProvider struct {
	Provider       domain.ProviderName
	ResultsByQuery map[string][]domain.SearchResult
}

func (p *QueryFakeProvider) Name() domain.ProviderName { return p.Provider }

func (p *QueryFakeProvider) Search(_ context.Context, query string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return p.ResultsByQuery[query], nil
}

func (p *QueryFakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func CaptureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func LoggedEvent(t *testing.T, buf *bytes.Buffer, msg string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"`+msg+`"`) {
			return line
		}
	}
	t.Fatalf("expected a %q event, got:\n%s", msg, buf.String())
	return ""
}
