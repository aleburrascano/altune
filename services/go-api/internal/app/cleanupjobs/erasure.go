package cleanupjobs

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func EraseDiscoveryRowsOfDeletedIdentities(ctx context.Context, erasers []search.DeletedUserDataEraser) error {
	var failures []error
	erased, idle := int64(0), 0
	for _, eraser := range erasers {
		rows, err := eraser.EraseRowsOfDeletedIdentities(ctx)
		if errors.Is(err, search.ErrIdentityStoreUnavailable) {
			slog.WarnContext(ctx, "discovery.deleted_identity_sweep_idle", logging.KeyError, err)
			idle++
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("erase discovery rows of deleted identities: %w", err))
			continue
		}
		erased += rows
	}
	if idle > 0 && idle == len(erasers) {
		failures = append(failures, fmt.Errorf("discovery erasure sweep idle, every eraser unavailable: %w", search.ErrIdentityStoreUnavailable))
	}
	logDiscoveryErasureSweep(ctx, erased)
	return errors.Join(failures...)
}

func logDiscoveryErasureSweep(ctx context.Context, erased int64) {
	if erased == 0 {
		return
	}
	slog.InfoContext(ctx, "discovery.deleted_identity_rows_erased", "rows", erased)
}
