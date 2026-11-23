package housekeeping

import (
	"altune/go-api/internal/discovery"
	"context"
	"time"
)

const unreleasedArtistsPerRun = 2

func RefreshDueUnreleased(svc *discovery.UnreleasedService) func(context.Context) error {
	return func(ctx context.Context) error {
		return svc.RefreshDue(ctx, time.Now().UTC(), unreleasedArtistsPerRun)
	}
}
