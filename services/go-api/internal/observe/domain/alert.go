package domain

import "context"

type Severity int

const (
	SeverityFix Severity = iota
	SeverityLog
	SeveritySignal
)

type Alert struct {
	Title    string
	Message  string
	Severity Severity
}

type Condition struct {
	Key  string
	Eval func(ctx context.Context) *Alert
}
