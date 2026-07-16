package app

import (
	"context"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

// inspectionSearchLimit caps test-search results — plenty to eyeball correctness.
const inspectionSearchLimit = 30

// searchInspector adapts the live discovery Service to adminHandler.SearchInspector
// so the Mission Control test-search runs the real pipeline (artwork + durable
// identity), bypassing the result cache, with no telemetry written.
type searchInspector struct {
	svc *discoveryService.Service
}

func (a *App) buildSearchInspector(svc *discoveryService.Service) adminHandler.SearchInspector {
	return &searchInspector{svc: svc}
}

// InspectSearch satisfies adminHandler.SearchInspector.
func (si *searchInspector) InspectSearch(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error) {
	kindSet := parseRerunKinds(kinds) // reused: defaults to all kinds when empty
	sq, err := domain.NewSearchQuery(query, kindSet, inspectionSearchLimit)
	if err != nil {
		return nil, err
	}
	return requeststore.ProjectResults(si.svc.InspectSearch(ctx, sq)), nil
}
