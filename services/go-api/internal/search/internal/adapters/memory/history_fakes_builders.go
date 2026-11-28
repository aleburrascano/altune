package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
)

func NewClearHistory(t TB, eraser ports.HistoryEraser) *service.ClearSearchHistoryService {
	t.Helper()
	svc, err := service.NewClearSearchHistoryService(eraser, clock.Now)
	if err != nil {
		t.Fatalf("history.NewClearSearchHistoryService: %v", err)
	}
	return svc
}

func NewListHistory(t TB, reader ports.HistoryReader) *service.ListSearchHistoryService {
	t.Helper()
	svc, err := service.NewListSearchHistoryService(reader)
	if err != nil {
		t.Fatalf("history.NewListSearchHistoryService: %v", err)
	}
	return svc
}
