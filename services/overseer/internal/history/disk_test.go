package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAShortSchemaTimeoutMakesOpenFallBackToUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	store := Open(path, WithSchemaTimeout(time.Nanosecond))
	t.Cleanup(func() { _ = store.Close() })

	if _, ok := store.(unavailable); !ok {
		t.Fatalf("Open under a 1ns schema timeout returned %T, want unavailable", store)
	}
}

func TestTheDefaultSchemaTimeoutKeepsHistoryAvailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	store := Open(path)
	t.Cleanup(func() { _ = store.Close() })

	if _, ok := store.(*disk); !ok {
		t.Fatalf("Open with the default schema timeout returned %T, want *disk", store)
	}
}

func TestANonPositiveSchemaTimeoutKeepsTheDefault(t *testing.T) {
	d := &disk{schemaTimeout: schemaTimeout}

	WithSchemaTimeout(0)(d)

	if d.schemaTimeout != schemaTimeout {
		t.Fatalf("schemaTimeout = %v after a zero option, want %v", d.schemaTimeout, schemaTimeout)
	}
}
