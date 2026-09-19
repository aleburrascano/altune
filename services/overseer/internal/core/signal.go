package core

import "time"

// Signal is one datum a bucket collects about the watched app. It is
// deliberately generic: the tracer bullet only needs a timestamp, a kind and a
// short human-readable text so any bucket can share the same bounded Store.
// Richer, bucket-specific payloads are a later leaf's concern.
type Signal struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	// CorrID ties this datum to the go-api request that produced it, so a signal,
	// a go-api log line and a failed read for one request line up. Empty when the
	// source carried none.
	CorrID string `json:"corrId,omitempty"`
}
