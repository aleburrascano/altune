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
}
