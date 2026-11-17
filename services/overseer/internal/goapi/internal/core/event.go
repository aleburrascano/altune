package core

import "time"

type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

type LogRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"attrs,omitempty"`
}
