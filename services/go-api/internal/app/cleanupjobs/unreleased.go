package cleanupjobs

import (
	"altune/go-api/internal/search"
	"context"
	"time"
)

const unreleasedArtistsPerRun = 2

func RefreshDueUnreleased(svc *search.UnreleasedService) func(context.Context) error {
	return func(ctx context.Context) error {
		return svc.RefreshDue(ctx, time.Now().UTC(), unreleasedArtistsPerRun)
	}
}
