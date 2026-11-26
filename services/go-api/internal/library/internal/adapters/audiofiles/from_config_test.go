package audiofiles_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFromConfigScopesKeysWhenPrefixSet(t *testing.T) {
	dir := t.TempDir()
	store, err := library.NewAudioStoreFromConfig(&config.Config{MusicDir: dir, AudioKeyPrefix: "staging/"}, slog.Default())
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}

	outsideRef := "prod-user/a/b/c.mp3"
	outsidePath := filepath.Join(dir, outsideRef)
	if err := os.MkdirAll(filepath.Dir(outsidePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(context.Background(), outsideRef); err != nil {
		t.Fatalf("delete outside the prefix should no-op: %v", err)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("file outside the prefix must survive: %v", err)
	}
}

func TestFromConfigFailsWhenOCIConfigIsBroken(t *testing.T) {
	cfg := &config.Config{
		OCIS3Endpoint:  "https://objectstorage.example.com/with/a/path",
		OCIS3AccessKey: "key",
		OCIS3SecretKey: "secret",
		OCIS3Bucket:    "bucket",
		MusicDir:       t.TempDir(),
	}

	store, err := library.NewAudioStoreFromConfig(cfg, slog.Default())
	if err == nil {
		t.Fatalf("broken OCI config must not fall back to a local store, got %v", store)
	}
	if store != nil {
		t.Fatalf("expected nil store alongside error, got %v", store)
	}
	if !strings.Contains(err.Error(), "OCI S3") {
		t.Errorf("error %q does not name the OCI cause", err)
	}
}

func TestFromConfigRejectsMisconfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"no backend", &config.Config{}, "MUSIC_DIR"},
		{"partial OCI", &config.Config{OCIS3Endpoint: "https://e", OCIS3AccessKey: "k", OCIS3SecretKey: "s"}, "OCI_S3_BUCKET"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := library.NewAudioStoreFromConfig(tt.cfg, slog.Default())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error naming %q, got %v", tt.want, err)
			}
		})
	}
}

func TestFromConfig_MusicDirTimeoutCapsAStalledOpenWithoutACallerDeadline(t *testing.T) {
	dir := t.TempDir()
	store, err := library.NewAudioStoreFromConfig(&config.Config{MusicDir: dir, MusicDirTimeout: 100 * time.Millisecond}, slog.Default())
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	makeStalledAudio(t, dir, "stalled.opus")

	start := time.Now()
	err = streamWithHangGuard(t, context.Background(), store, "stalled.opus")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected MUSIC_DIR_TIMEOUT to cap the open with context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stream returned after %s, want roughly the 100ms MUSIC_DIR_TIMEOUT", elapsed)
	}
}

func TestFromConfig_AnUnsetMusicDirTimeoutKeepsTheDefaultCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.opus"), []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := library.NewAudioStoreFromConfig(&config.Config{MusicDir: dir}, slog.Default())
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}

	exists, err := store.Exists(context.Background(), "song.opus")

	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v; want true with no error under the default cap", exists, err)
	}
}

func TestFromConfig_AMisconfiguredStoreFailsFastNamingWhatIsMissing(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		wantContains []string
	}{
		{
			name:         "no backend configured",
			cfg:          &config.Config{},
			wantContains: []string{"MUSIC_DIR", "OCI_S3_ENDPOINT"},
		},
		{
			name: "incomplete OCI S3 names the missing variable",
			cfg: &config.Config{
				OCIS3Endpoint:  "https://objectstorage.example.com",
				OCIS3AccessKey: "key",
				OCIS3SecretKey: "secret",
			},
			wantContains: []string{"OCI_S3_BUCKET", "incomplete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := library.NewAudioStoreFromConfig(tt.cfg, slog.Default())
			if err == nil {
				t.Fatalf("expected startup error for misconfigured audio store, got store=%v", store)
			}
			if store != nil {
				t.Fatalf("expected nil store alongside error, got %v", store)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err.Error(), want)
				}
			}
		})
	}
}

func TestFromConfig_AnEmptyKeyPrefixLeavesTheStoreUnscoped(t *testing.T) {
	dir := t.TempDir()
	store, err := library.NewAudioStoreFromConfig(&config.Config{MusicDir: dir}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref := "u/a/b/c.mp3"
	path := filepath.Join(dir, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := store.Delete(context.Background(), ref); err != nil {
		t.Fatalf("expected delete to succeed with no prefix configured, got error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be deleted when no prefix is configured, got: %v", err)
	}
}
