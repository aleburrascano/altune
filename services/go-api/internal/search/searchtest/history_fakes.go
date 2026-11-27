package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/history"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type (
	FakeHistoryWriter = memory.FakeHistoryWriter
	FakeHistoryReader = memory.FakeHistoryReader
	FakeHistoryEraser = memory.FakeHistoryEraser
)

func NewClearHistory(t testing.TB, eraser ports.HistoryEraser) *history.ClearSearchHistoryService {
	t.Helper()
	svc, err := history.NewClearSearchHistoryService(eraser, clock.Now)
	if err != nil {
		t.Fatalf("history.NewClearSearchHistoryService: %v", err)
	}
	return svc
}

func NewListHistory(t testing.TB, reader ports.HistoryReader) *history.ListSearchHistoryService {
	t.Helper()
	svc, err := history.NewListSearchHistoryService(reader)
	if err != nil {
		t.Fatalf("history.NewListSearchHistoryService: %v", err)
	}
	return svc
}
