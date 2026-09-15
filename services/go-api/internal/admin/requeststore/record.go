package requeststore

import "time"

type RequestRecord struct {
	CorrID    string     `json:"corr_id"`
	StartedAt time.Time  `json:"started_at"`
	Exchanges []Exchange `json:"exchanges"`

	Query     string          `json:"query,omitempty"`
	Kinds     []string        `json:"kinds,omitempty"`
	User      string          `json:"user,omitempty"`
	Providers []ProviderTrace `json:"providers,omitempty"`
	Final     []ResultRow     `json:"final,omitempty"`

	Detail *DetailTrace `json:"detail,omitempty"`

	// bytes is the record's whole charge against the store budget: exchange
	// bodies plus searchBytes and detailBytes, the current estimated sizes of
	// the search and detail traces (held apart so re-recording replaces them).
	bytes       int
	searchBytes int
	detailBytes int
	// born is the monotonic-bearing instant retention ages the record from;
	// StartedAt is its UTC rendering, which has lost the monotonic reading.
	born time.Time
}
