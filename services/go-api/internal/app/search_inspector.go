package app

import (
	"context"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

const inspectionSearchLimit = 30

type searchInspector struct {
	svc *discoveryService.Service
}

func (a *App) buildSearchInspector(svc *discoveryService.Service) adminHandler.SearchInspector {
	return &searchInspector{svc: svc}
}

func (si *searchInspector) InspectSearch(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error) {
	kindSet, err := parseRerunKinds(kinds)
	if err != nil {
		return nil, err
	}
	sq, err := domain.NewSearchQuery(query, kindSet, inspectionSearchLimit)
	if err != nil {
		return nil, err
	}
	results, statuses := si.svc.InspectSearchWithStatuses(ctx, sq)
	if discoveryService.AllProvidersFailed(statuses) {
		return nil, discoveryService.ErrAllProvidersFailed
	}
	return requeststore.ProjectResults(results), nil
}
