package commands

import (
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
)

func buildAudioStoreForCLI(cfg *config.Config) ports.AudioStore {
	if cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(
			cfg.OCIS3Endpoint, cfg.OCIS3AccessKey, cfg.OCIS3SecretKey,
			cfg.OCIS3Bucket, cfg.OCIS3Region,
		)
		if err == nil {
			return store
		}
	}
	if cfg.MusicDir != "" {
		return storage.NewFilesystemAudioStore(cfg.MusicDir)
	}
	return nil
}
