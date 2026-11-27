package library

import storage "altune/go-api/internal/library/internal/adapters/audiofiles"

const MaxInFlightFilesystemCalls = storage.MaxInFlightFilesystemCalls

var (
	NewAudioStoreFromConfig = storage.NewAudioStoreFromConfig
	NewFilesystemAudioStore = storage.NewFilesystemAudioStore
	NewScopedAudioBackend   = storage.NewScopedAudioBackend
)

type (
	NoAudioLinks = storage.NoAudioLinks
	NoAgeListing = storage.NoAgeListing
)
