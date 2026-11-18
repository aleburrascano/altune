package catalogtest

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

type TrackRepo = memory.TrackRepo

var NewTrackRepo = memory.NewTrackRepo
