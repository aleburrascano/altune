package httptrace

import "altune/go-api/internal/shared/httptrace/internal/adapters/transport"

type (
	Exchange = transport.Exchange
	Recorder = transport.Recorder
	Replayer = transport.Replayer
)

var (
	NewRecorder       = transport.NewRecorder
	NewReplayer       = transport.NewReplayer
	NewStickyReplayer = transport.NewStickyReplayer
)
