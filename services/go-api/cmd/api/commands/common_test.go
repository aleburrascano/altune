package commands

import (
	"altune/go-api/internal/shared/config"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAudioStoreFromConfigKeepsCommandsInsideTheKeyPrefix(t *testing.T) {
	musicDir := t.TempDir()
	prodRef := "11111111-1111-1111-1111-111111111111/Artist/Album/Song.mp3"
	prodFile := filepath.Join(musicDir, prodRef)
	if err := os.MkdirAll(filepath.Dir(prodFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prodFile, []byte("prod audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "new.mp3")
	if err := os.WriteFile(source, []byte("new audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewAudioStoreFromConfig(&config.Config{MusicDir: musicDir, AudioKeyPrefix: "staging/"})
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}
	ctx := context.Background()

	if err := store.Delete(ctx, prodRef); err != nil {
		t.Fatalf("Delete outside the prefix = %v, want a nil no-op", err)
	}
	if _, err := os.Stat(prodFile); err != nil {
		t.Fatalf("Delete outside the prefix removed the object: %v", err)
	}
	if err := store.Store(ctx, source, "22222222-2222-2222-2222-222222222222/A/B/C.mp3"); err == nil {
		t.Fatal("Store outside the prefix succeeded, want an error")
	}
	if err := store.Store(ctx, source, "staging/22222222-2222-2222-2222-222222222222/A/B/C.mp3"); err != nil {
		t.Fatalf("Store inside the prefix = %v, want nil", err)
	}
}

func TestNewAudioStoreFromConfigLeavesAnUnprefixedStoreUnscoped(t *testing.T) {
	musicDir := t.TempDir()
	source := filepath.Join(t.TempDir(), "new.mp3")
	if err := os.WriteFile(source, []byte("new audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewAudioStoreFromConfig(&config.Config{MusicDir: musicDir})
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}
	if err := store.Store(context.Background(), source, "33333333-3333-3333-3333-333333333333/A/B/C.mp3"); err != nil {
		t.Fatalf("Store without a prefix = %v, want nil", err)
	}
}
