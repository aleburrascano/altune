package core

const sloMinTotal = 200

var (
	SearchAvailabilitySLO = SLO{Journey: "find music", Name: "search answers without a server error", Target: 0.995, MinTotal: sloMinTotal}
	SearchLatencySLO      = SLO{Journey: "find music", Name: "search answers within 1 s", Target: 0.95, MinTotal: sloMinTotal}
	SongAcquiredSLO       = SLO{Journey: "get a song", Name: "a requested song is acquired", Target: 0.9, MinTotal: sloMinTotal}
)
