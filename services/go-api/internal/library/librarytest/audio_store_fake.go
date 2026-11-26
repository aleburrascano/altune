package librarytest

import memory "altune/go-api/internal/library/internal/adapters/memory"

type AudioStore = memory.AudioStore

var NewAudioStore = memory.NewAudioStore
