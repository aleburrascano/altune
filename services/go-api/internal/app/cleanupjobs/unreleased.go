package cleanupjobs

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"context"
)

const unreleasedArtistsPerRun = 2

func RefreshDueUnreleased(svc *search.UnreleasedService, clk clock.Func) func(context.Context) error {
	return func(ctx context.Context) error {
		return svc.RefreshDue(ctx, clk.UTC(), unreleasedArtistsPerRun)
	}
}
