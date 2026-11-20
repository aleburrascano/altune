package domain

import (
	"context"
	"fmt"
)

type Severity int

const (
	SeverityFix Severity = iota
	SeverityLog
	SeveritySignal
)

func (s Severity) String() string {
	switch s {
	case SeverityFix:
		return "fix"
	case SeverityLog:
		return "log"
	case SeveritySignal:
		return "signal"
	}
	return fmt.Sprintf("unknown (%d)", int(s))
}

type Alert struct {
	Title    string
	Message  string
	Severity Severity
}

type Condition struct {
	Key  string
	Eval func(ctx context.Context) *Alert
}
