package timingtest

import "altune/go-api/internal/shared/timingtest/internal/adapters/slowconn"

var (
	NewSmallBufferServer = slowconn.NewServer
	SendGet              = slowconn.Get
	ReadResponse         = slowconn.ReadResponse
)
