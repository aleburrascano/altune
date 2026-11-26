package librarytest

import memory "altune/go-api/internal/library/internal/adapters/memory"

type TrackRepo = memory.TrackRepo

var NewTrackRepo = memory.NewTrackRepo
