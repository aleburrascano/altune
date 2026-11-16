package catalog

import storage "altune/go-api/internal/catalog/internal/adapters/storage"

var (
	FromConfig              = storage.FromConfig
	NewFilesystemAudioStore = storage.NewFilesystemAudioStore
)
