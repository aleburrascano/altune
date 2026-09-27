package commands

import (
	"altune/go-api/internal/catalog/ports"
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

func TestNewAudioStoreFromConfigForwardsAudioAgeListerForSweep(t *testing.T) {
	musicDir := t.TempDir()
	stagingRef := "staging/11111111-1111-1111-1111-111111111111/Artist/Album/Song.mp3"
	stagingFile := filepath.Join(musicDir, stagingRef)
	if err := os.MkdirAll(filepath.Dir(stagingFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagingFile, []byte("staging audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewAudioStoreFromConfig(&config.Config{MusicDir: musicDir, AudioKeyPrefix: "staging/"})
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}

	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		t.Fatal("expected the staging-scoped store built with AudioKeyPrefix=staging/ to satisfy ports.AudioAgeLister")
	}
	objects, err := lister.ListWithAge(context.Background(), "staging/")
	if err != nil {
		t.Fatalf("ListWithAge: %v", err)
	}
	if len(objects) != 1 || objects[0].AudioRef != stagingRef {
		t.Errorf("got %v, want a single entry for %q", objects, stagingRef)
	}
}

func TestNewAudioStoreFromConfigCopierStaysInsidePrefixSoPromoteNeedsTheUnscopedStore(t *testing.T) {
	musicDir := t.TempDir()
	stagingUser := "11111111-1111-1111-1111-111111111111"
	prodUser := "22222222-2222-2222-2222-222222222222"
	srcRef := "staging/" + stagingUser + "/Artist/Album/Song.mp3"
	srcFile := filepath.Join(musicDir, srcRef)
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("staging audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	prodRef := prodUser + "/Artist/Album/Song.mp3"

	scoped, err := NewAudioStoreFromConfig(&config.Config{MusicDir: musicDir, AudioKeyPrefix: "staging/"})
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}
	scopedCopier, ok := scoped.(ports.AudioCopier)
	if !ok {
		t.Fatal("expected the staging-scoped store to satisfy ports.AudioCopier")
	}
	if err := scopedCopier.Copy(context.Background(), srcRef, prodRef); err == nil {
		t.Fatal("expected the scoped copier to refuse copying out to a prod key, want an error")
	}

	unscoped, err := NewAudioStoreFromConfig(&config.Config{MusicDir: musicDir})
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}
	unscopedCopier, ok := unscoped.(ports.AudioCopier)
	if !ok {
		t.Fatal("expected the unscoped store promote-staging runs against to satisfy ports.AudioCopier")
	}
	if err := unscopedCopier.Copy(context.Background(), srcRef, prodRef); err != nil {
		t.Fatalf("Copy through the unscoped store = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(musicDir, prodRef)); err != nil {
		t.Fatalf("expected the copy to land at the prod ref: %v", err)
	}
}
