package slowclienttest

import "altune/go-api/internal/shared/slowclienttest/internal/adapters/slowconn"

var (
	NewServer    = slowconn.NewServer
	Get          = slowconn.Get
	ReadResponse = slowconn.ReadResponse
)
