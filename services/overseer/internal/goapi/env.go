package goapi

import (
	"context"
	"os"
	"strings"
)

func BaseURLFromEnv() (string, bool) {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	return base, base != ""
}

type NullEventSource struct {
	events chan Event
	down   error
}

func NewNullEventSource(down error) *NullEventSource {
	return &NullEventSource{events: make(chan Event), down: down}
}

func (n *NullEventSource) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (n *NullEventSource) Events() <-chan Event          { return n.events }
func (n *NullEventSource) Status() Status                { return StatusDown }

func (n *NullEventSource) LastError() error {
	return &SourceDownError{Op: "stream", Err: n.down}
}
