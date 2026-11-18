package catalogtest

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

type AudioStore = memory.AudioStore

var NewAudioStore = memory.NewAudioStore
