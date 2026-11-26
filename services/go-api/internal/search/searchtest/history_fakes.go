package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type (
	FakeHistoryWriter = memory.FakeHistoryWriter
	FakeHistoryReader = memory.FakeHistoryReader
	FakeHistoryEraser = memory.FakeHistoryEraser
)
