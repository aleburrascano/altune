package slowclient

import "altune/go-api/internal/shared/slowclient/internal/adapters/slowconn"

var (
	NewServer    = slowconn.NewServer
	Get          = slowconn.Get
	ReadResponse = slowconn.ReadResponse
)
