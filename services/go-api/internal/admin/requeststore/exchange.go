package requeststore

import "time"

type Exchange struct {
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	RespBody  string    `json:"response_body"`
	Truncated bool      `json:"truncated,omitempty"`
	Err       string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}
