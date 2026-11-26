package search

import history "altune/go-api/internal/search/internal/core/history"

var (
	ErrHistoryClockRequired           = history.ErrClockRequired
	ErrHistoryCollaboratorRequired    = history.ErrCollaboratorRequired
	MustNewClearSearchHistoryService  = history.MustNewClearSearchHistoryService
	MustNewRecordSearchHistoryService = history.MustNewRecordSearchHistoryService
	NewClearSearchHistoryService      = history.NewClearSearchHistoryService
	NewListSearchHistoryService       = history.NewListSearchHistoryService
	NewRecordSearchHistoryService     = history.NewRecordSearchHistoryService
)

type (
	ClearSearchHistoryService = history.ClearSearchHistoryService
	ListSearchHistoryService  = history.ListSearchHistoryService
)
