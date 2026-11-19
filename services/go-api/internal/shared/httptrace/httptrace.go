package httptrace

import "altune/go-api/internal/shared/httptrace/internal/adapters/transport"

type (
	Exchange = transport.Exchange
	Recorder = transport.Recorder
)

var (
	NewRecorder = transport.NewRecorder
	NewReplayer = transport.NewReplayer
)
