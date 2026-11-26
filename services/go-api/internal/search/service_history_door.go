package search

import history "altune/go-api/internal/search/internal/core/history"

var (
	NewClearSearchHistoryService   = history.NewClearSearchHistoryService
	NewListSearchHistoryService    = history.NewListSearchHistoryService
	NewRecordSearchHistoryService  = history.NewRecordSearchHistoryService
	ErrHistoryCollaboratorRequired = history.ErrCollaboratorRequired
)

type (
	ClearSearchHistoryService = history.ClearSearchHistoryService
	ListSearchHistoryService  = history.ListSearchHistoryService
)
