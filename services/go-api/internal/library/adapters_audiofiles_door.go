package library

import storage "altune/go-api/internal/library/internal/adapters/audiofiles"

var (
	NewAudioStoreFromConfig = storage.NewAudioStoreFromConfig
	NewFilesystemAudioStore = storage.NewFilesystemAudioStore
	NewScopedAudioStore     = storage.NewScopedAudioStore
)
