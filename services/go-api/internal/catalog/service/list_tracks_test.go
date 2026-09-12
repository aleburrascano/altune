package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"strings"
	"testing"
)

func TestListTracksService_RejectsNegativeOffset(t *testing.T) {
	svc := NewListTracksService(catalogtest.NewTrackRepo())

	out, err := svc.Execute(context.Background(), testUserId(), domain.LibraryQuery{Offset: -1})

	if err == nil {
		t.Fatalf("expected a validation error for a negative offset, got nil (out=%+v)", out)
	}
	shared.AssertValidationError(t, err)
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
	}
}
