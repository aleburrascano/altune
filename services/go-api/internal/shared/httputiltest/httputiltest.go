package httputiltest

import "altune/go-api/internal/shared/httputiltest/internal/adapters/slowconn"

var (
	NewServer    = slowconn.NewServer
	Get          = slowconn.Get
	ReadResponse = slowconn.ReadResponse
)
