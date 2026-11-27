package filefloor_test

import (
	"altune/go-api/internal/shared/events/internal/adapters/filefloor"
	"altune/go-api/internal/shared/events/internal/service/idfloor"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func floorPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "altune-event-id-floor")
}

func TestStore_LoadsWhatItSaved(t *testing.T) {
	store := filefloor.New(floorPath(t))

	if err := store.Save([]byte("1234")); err != nil {
		t.Fatalf("Save = %v", err)
	}
	got, err := store.Load()

	if err != nil || string(got) != "1234" {
		t.Fatalf("Load = (%q, %v), want 1234", got, err)
	}
	if _, statErr := os.Stat(store.Location() + ".tmp"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the temporary file was left behind: %v", statErr)
	}
}

func TestStore_LoadingAMissingFileIsNotFound(t *testing.T) {
	_, err := filefloor.New(floorPath(t)).Load()

	if !errors.Is(err, idfloor.ErrNotFound) {
		t.Fatalf("Load = %v, want ErrNotFound", err)
	}
}

func TestStore_LoadingADirectoryIsAnErrorOtherThanNotFound(t *testing.T) {
	_, err := filefloor.New(t.TempDir()).Load()

	if err == nil || errors.Is(err, idfloor.ErrNotFound) {
		t.Fatalf("Load = %v, want a read error that is not ErrNotFound", err)
	}
}

func TestStore_SavingIntoAMissingDirectoryFails(t *testing.T) {
	store := filefloor.New(filepath.Join(t.TempDir(), "missing-dir", "floor"))

	if err := store.Save([]byte("1")); err == nil {
		t.Fatal("Save into a missing directory = nil, want an error")
	}
}

func TestStore_SavingOverADirectoryFails(t *testing.T) {
	if err := filefloor.New(t.TempDir()).Save([]byte("1")); err == nil {
		t.Fatal("Save over a directory = nil, want an error")
	}
}
