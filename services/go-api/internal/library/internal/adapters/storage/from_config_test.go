package storage_test

import (
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFromConfigScopesKeysWhenPrefixSet(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewAudioStoreFromConfig(&config.Config{MusicDir: dir, AudioKeyPrefix: "staging/"}, slog.Default(), librarytest.DiscardFSCallMetrics())
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

	store, err := storage.NewAudioStoreFromConfig(cfg, slog.Default(), librarytest.DiscardFSCallMetrics())
	if err == nil {
		t.Fatalf("broken OCI config must not fall back to a local store, got %v", store)
	}
	if store.AudioStore != nil || store.Links != nil || store.Ages != nil {
		t.Fatalf("expected an empty backend alongside the error, got %v", store)
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
			_, err := storage.NewAudioStoreFromConfig(tt.cfg, slog.Default(), librarytest.DiscardFSCallMetrics())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error naming %q, got %v", tt.want, err)
			}
		})
	}
}

func TestFromConfig_MusicDirTimeoutCapsAStalledOpenWithoutACallerDeadline(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewAudioStoreFromConfig(&config.Config{MusicDir: dir, MusicDirTimeout: 100 * time.Millisecond}, slog.Default(), librarytest.DiscardFSCallMetrics())
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

type countingFSCallMetrics struct{ refused, abandoned atomic.Int64 }

func (m *countingFSCallMetrics) FSCallRefused()   { m.refused.Add(1) }
func (m *countingFSCallMetrics) FSCallAbandoned() { m.abandoned.Add(1) }

func TestFromConfig_AStalledMountRefusesNewCallsInsteadOfPilingUpGoroutines(t *testing.T) {
	dir := t.TempDir()
	metrics := &countingFSCallMetrics{}
	store, err := storage.NewAudioStoreFromConfig(&config.Config{MusicDir: dir, MusicDirTimeout: 50 * time.Millisecond}, slog.Default(), metrics)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	makeStalledAudio(t, dir, "stalled.opus")
	const callers = storage.MaxInFlightFilesystemCalls + 5
	errs := make(chan error, callers)
	for range callers {
		go func() {
			rc, _, err := store.Stream(context.Background(), "stalled.opus")
			if err == nil {
				rc.Close()
			}
			errs <- err
		}()
	}

	timedOut, refused := 0, 0
	for range callers {
		select {
		case err := <-errs:
			switch {
			case errors.Is(err, domain.ErrStorageStalled):
				refused++
			case errors.Is(err, context.DeadlineExceeded):
				timedOut++
			default:
				t.Fatalf("Stream on a stalled mount = %v, want a deadline or ErrStorageStalled", err)
			}
		case <-time.After(stallHangGuard):
			t.Fatal("a Stream call on a stalled mount never returned")
		}
	}

	if timedOut != storage.MaxInFlightFilesystemCalls || refused != callers-storage.MaxInFlightFilesystemCalls {
		t.Fatalf("timed out %d and refused %d, want %d timed out and %d refused", timedOut, refused, storage.MaxInFlightFilesystemCalls, callers-storage.MaxInFlightFilesystemCalls)
	}
	if got := metrics.refused.Load(); got != int64(refused) {
		t.Errorf("refused-call metric = %d, want %d", got, refused)
	}
	if got := metrics.abandoned.Load(); got != int64(timedOut) {
		t.Errorf("abandoned-call metric = %d, want %d", got, timedOut)
	}
}

func TestNewFilesystemAudioStore_RefusesNilFSCallMetrics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("construction without fs call metrics succeeded, want a panic")
		}
	}()

	storage.NewFilesystemAudioStore(t.TempDir(), nil)
}

func TestFromConfig_AnUnsetMusicDirTimeoutKeepsTheDefaultCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.opus"), []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewAudioStoreFromConfig(&config.Config{MusicDir: dir}, slog.Default(), librarytest.DiscardFSCallMetrics())
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
			store, err := storage.NewAudioStoreFromConfig(tt.cfg, slog.Default(), librarytest.DiscardFSCallMetrics())
			if err == nil {
				t.Fatalf("expected startup error for misconfigured audio store, got store=%v", store)
			}
			if store.AudioStore != nil || store.Links != nil || store.Ages != nil {
				t.Fatalf("expected an empty backend alongside the error, got %v", store)
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
	store, err := storage.NewAudioStoreFromConfig(&config.Config{MusicDir: dir}, slog.Default(), librarytest.DiscardFSCallMetrics())
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
