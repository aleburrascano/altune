package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioStore_StoreMakesTheRefExistAndDeleteRemovesIt(t *testing.T) {
	ctx := context.Background()
	store := memory.NewAudioStore()
	source := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(source, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	if exists, _ := store.Exists(ctx, "u/a.mp3"); exists {
		t.Fatal("Exists before Store = true, want false")
	}
	if err := store.Store(ctx, source, "u/a.mp3"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if exists, _ := store.Exists(ctx, "u/a.mp3"); !exists {
		t.Error("Exists after Store = false, want true")
	}
	if err := store.Delete(ctx, "u/a.mp3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists, _ := store.Exists(ctx, "u/a.mp3"); exists {
		t.Error("Exists after Delete = true, want false")
	}
}

func TestAudioStore_StoreRefusesAMissingSourceFile(t *testing.T) {
	if err := memory.NewAudioStore().Store(context.Background(), filepath.Join(t.TempDir(), "missing.mp3"), "u/a.mp3"); err == nil {
		t.Error("Store of a missing file = nil, want an error")
	}
}
