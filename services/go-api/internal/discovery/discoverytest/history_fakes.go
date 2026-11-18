package discoverytest

import "altune/go-api/internal/discovery/internal/adapters/memory"

type (
	FakeHistoryWriter = memory.FakeHistoryWriter
	FakeHistoryReader = memory.FakeHistoryReader
	FakeHistoryEraser = memory.FakeHistoryEraser
)
