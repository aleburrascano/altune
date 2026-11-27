package audiofiles

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/shared/config"
	"fmt"
	"log/slog"
	"strings"
)

const ociS3ConfigKeys = 4

func NewAudioStoreFromConfig(cfg *config.Config, logger *slog.Logger, metrics ports.FSCallMetrics) (ports.AudioBackend, error) {
	backend, err := unscopedFromConfig(cfg, logger, metrics)
	if err != nil {
		return ports.AudioBackend{}, err
	}
	if cfg.AudioKeyPrefix == "" {
		return backend, nil
	}
	logger.Info("audio store: scoped to key prefix", "prefix", cfg.AudioKeyPrefix)
	return NewScopedAudioBackend(backend, cfg.AudioKeyPrefix), nil
}

func unscopedFromConfig(cfg *config.Config, logger *slog.Logger, metrics ports.FSCallMetrics) (ports.AudioBackend, error) {
	if missing := missingOCIS3Keys(cfg); len(missing) > 0 && len(missing) < ociS3ConfigKeys {
		return ports.AudioBackend{}, missingAudioStoreError(missing)
	}
	if cfg.HasOCIS3() {
		store, err := ociFromConfig(cfg, logger)
		if err != nil {
			return ports.AudioBackend{}, err
		}
		return ports.AudioBackend{AudioStore: store, Links: store, Ages: store}, nil
	}
	if cfg.MusicDir != "" {
		logger.Info("audio store: filesystem", "dir", cfg.MusicDir)
		store := newFilesystemAudioStore(cfg.MusicDir, cfg.MusicDirTimeout, metrics)
		return ports.AudioBackend{AudioStore: store, Links: NoAudioLinks{}, Ages: store}, nil
	}
	return ports.AudioBackend{}, missingAudioStoreError(nil)
}

func ociFromConfig(cfg *config.Config, logger *slog.Logger) (*ObjectStorageAudioStore, error) {
	store, err := NewObjectStorageAudioStore(ObjectStorageConfig{
		Endpoint:  cfg.OCIS3Endpoint,
		AccessKey: cfg.OCIS3AccessKey,
		SecretKey: cfg.OCIS3SecretKey,
		Bucket:    cfg.OCIS3Bucket,
		Region:    cfg.OCIS3Region,
	})
	if err != nil {
		return nil, fmt.Errorf("audio store: OCI S3 is configured but failed to initialize: %w", err)
	}
	logger.Info("audio store: OCI Object Storage")
	return store, nil
}

func missingOCIS3Keys(cfg *config.Config) []string {
	var missing []string
	for _, key := range []struct{ name, value string }{
		{"OCI_S3_ENDPOINT", cfg.OCIS3Endpoint},
		{"OCI_S3_ACCESS_KEY", cfg.OCIS3AccessKey},
		{"OCI_S3_SECRET_KEY", cfg.OCIS3SecretKey},
		{"OCI_S3_BUCKET", cfg.OCIS3Bucket},
	} {
		if key.value == "" {
			missing = append(missing, key.name)
		}
	}
	return missing
}

func missingAudioStoreError(missing []string) error {
	if len(missing) > 0 {
		return fmt.Errorf(
			"audio store: incomplete OCI S3 configuration, missing %s (set these for object storage, or set MUSIC_DIR for a filesystem store)",
			strings.Join(missing, ", "),
		)
	}
	return fmt.Errorf(
		"audio store: no backend configured; set MUSIC_DIR for a filesystem store, or all of OCI_S3_ENDPOINT, OCI_S3_ACCESS_KEY, OCI_S3_SECRET_KEY, OCI_S3_BUCKET for object storage",
	)
}
