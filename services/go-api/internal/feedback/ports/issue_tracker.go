package ports

import (
	"context"

	"altune/go-api/internal/feedback/domain"
)

type IssueRef struct {
	Number int
	URL    string
}

type IssueTracker interface {
	Create(ctx context.Context, report *domain.Report) (IssueRef, error)
}
