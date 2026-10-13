package storage

import (
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromConfigScopesKeysWhenPrefixSet(t *testing.T) {
	dir := t.TempDir()
	store, err := FromConfig(&config.Config{MusicDir: dir, AudioKeyPrefix: "staging/"}, slog.Default())
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

	store, err := FromConfig(cfg, slog.Default())
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
			_, err := FromConfig(tt.cfg, slog.Default())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error naming %q, got %v", tt.want, err)
			}
		})
	}
}
