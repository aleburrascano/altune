package housekeeping

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func EraseDiscoveryRowsOfDeletedIdentities(ctx context.Context, erasers []discovery.DeletedIdentityEraser) error {
	var failures []error
	erased, idle := int64(0), 0
	for _, eraser := range erasers {
		rows, err := eraser.EraseRowsOfDeletedIdentities(ctx)
		if errors.Is(err, discovery.ErrIdentityStoreUnavailable) {
			slog.WarnContext(ctx, "discovery.deleted_identity_sweep_idle", "error", err)
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
		failures = append(failures, fmt.Errorf("discovery erasure sweep idle, every eraser unavailable: %w", discovery.ErrIdentityStoreUnavailable))
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
