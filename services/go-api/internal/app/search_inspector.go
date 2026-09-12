package app

import (
	"context"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

const inspectionSearchLimit = 30

func inspectSearch(ctx context.Context, svc *discoveryService.Service, query string, kinds []string) ([]requeststore.ResultRow, error) {
	kindSet, err := parseSearchKinds(kinds)
	if err != nil {
		return nil, err
	}
	sq, err := domain.NewSearchQuery(query, kindSet, inspectionSearchLimit)
	if err != nil {
		return nil, err
	}
	results, statuses := svc.InspectSearchWithStatuses(ctx, sq)
	if discoveryService.AllProvidersFailed(statuses) {
		return nil, discoveryService.ErrAllProvidersFailed
	}
	return requeststore.ProjectResults(results), nil
}
