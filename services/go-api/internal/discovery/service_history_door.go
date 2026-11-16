package discovery

import history "altune/go-api/internal/discovery/service/history"

var (
	NewClearSearchHistoryService  = history.NewClearSearchHistoryService
	NewListSearchHistoryService   = history.NewListSearchHistoryService
	NewRecordSearchHistoryService = history.NewRecordSearchHistoryService
)
