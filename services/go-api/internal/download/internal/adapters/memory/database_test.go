package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"context"
	"testing"
	"time"
)

func TestDatabase_PrunesNothingBecauseItKeepsEveryRow(t *testing.T) {
	outcomes, rejections := memory.NewDatabase(time.Now).Retention()

	for name, pruner := range map[string]interface {
		Prune(context.Context, time.Time) (int64, error)
	}{"outcomes": outcomes, "rejections": rejections} {
		pruned, err := pruner.Prune(t.Context(), time.Now())

		if pruned != 0 || err != nil {
			t.Fatalf("%s Prune = %d, %v, want 0 and no error", name, pruned, err)
		}
	}
}

func TestDatabase_HandsOutTheSameUpgradeRecordsEachTime(t *testing.T) {
	db := memory.NewDatabase(time.Now)

	if db.Upgrades() != db.Upgrades() {
		t.Fatal("Upgrades returned a fresh store on the second call, so a write through one is lost to the other")
	}
}
