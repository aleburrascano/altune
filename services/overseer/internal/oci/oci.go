package oci

import (
	"context"
	"errors"
	"time"
)

type Spend struct {
	Amount      float64     `json:"amount"`
	Currency    string      `json:"currency"`
	PeriodStart time.Time   `json:"periodStart"`
	PeriodEnd   time.Time   `json:"periodEnd"`
	Lines       []SpendLine `json:"lines"`
}

type SpendLine struct {
	Service string  `json:"service"`
	Amount  float64 `json:"amount"`
}

type UsageClient interface {
	CurrentPeriodSpend(ctx context.Context) (Spend, error)
}

type SourceDownError struct {
	Err error
}

func (e *SourceDownError) Error() string {
	return "oci: usage-api unavailable: " + e.Err.Error()
}

func (e *SourceDownError) Unwrap() error { return e.Err }

func IsSourceDown(err error) bool {
	var sd *SourceDownError
	return errors.As(err, &sd)
}
