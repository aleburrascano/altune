package core

import "time"

type Signal struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	CorrID string    `json:"corrId,omitempty"`
}
