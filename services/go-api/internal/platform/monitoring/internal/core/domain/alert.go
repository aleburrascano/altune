package domain

import (
	"context"
	"fmt"
	"time"
)

type Severity int

const (
	SeverityActNow Severity = iota
	SeverityRecord
	SeverityWatch
)

func (s Severity) String() string {
	switch s {
	case SeverityActNow:
		return "fix"
	case SeverityRecord:
		return "log"
	case SeverityWatch:
		return "signal"
	}
	return fmt.Sprintf("unknown (%d)", int(s))
}

type Alert struct {
	Title    string
	Message  string
	Severity Severity
	Key      string
	FiredAt  time.Time
}

type Condition struct {
	Key  string
	Eval func(ctx context.Context) *Alert
}
