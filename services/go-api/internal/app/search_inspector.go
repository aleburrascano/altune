package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"

	discoveryService "altune/go-api/internal/discovery/service"
)

const inspectionSearchLimit = 30

// errInvalidInspectorInput classifies a reRun/inspectSearch/reRunDetail failure
// as the caller's fault (bad kinds, empty or oversized query), so the admin
// boundary can answer 400 instead of lumping it in with a provider outage.
var errInvalidInspectorInput = errors.New("invalid inspector input")

// classifiedError tags cause with a class sentinel for errors.Is while keeping
// cause's message verbatim, so the operator still sees the original wording.
type classifiedError struct {
	class error
	cause error
}

func (e classifiedError) Error() string   { return e.cause.Error() }
func (e classifiedError) Unwrap() []error { return []error{e.class, e.cause} }

func invalidInspectorInput(err error) error {
	return classifiedError{class: errInvalidInspectorInput, cause: err}
}

func inspectSearch(ctx context.Context, svc *discoveryService.Service, query string, kinds []string) ([]requeststore.ResultRow, error) {
	kindSet, err := parseSearchKinds(kinds)
	if err != nil {
		return nil, invalidInspectorInput(err)
	}
	sq, err := domain.NewSearchQuery(query, kindSet, inspectionSearchLimit)
	if err != nil {
		return nil, invalidInspectorInput(err)
	}
	results, statuses := svc.InspectSearchWithStatuses(ctx, sq)
	if discoveryService.AllProvidersFailed(statuses) {
		return nil, discoveryService.ErrAllProvidersFailed
	}
	return requeststore.ProjectResults(results), nil
}
