// Package oci is the Overseer's read-only client onto OCI's usage-api, the
// external source behind the Cost bucket's infra-spend half. The Overseer runs on
// an OCI box, so it authenticates by instance principal (no stored key) and calls
// exactly one usage-api read — RequestSummarizedUsages — to fetch current-period
// spend. It holds no mutating OCI operation and never touches infrastructure.
//
// The package is built around two seams so it is unit-testable with no OCI auth
// (which is impossible off an OCI instance):
//
//   - UsageClient is the seam the Cost bucket depends on: one method that returns
//     an already-summarised Spend. A fake drives the bucket in tests.
//   - usageAPI (unexported) is the minimal SDK surface the real Client depends on:
//     the single read RequestSummarizedUsages. Narrowing the SDK to one read both
//     documents that no mutating call exists and lets a fake drive the SDK→Spend
//     mapping in tests.
//
// Spend carries only aggregate figures and service names — never a tenancy OCID,
// compartment OCID or resource OCID — so nothing OCI-identifying can reach the
// rendered panel, by construction.
package oci

import (
	"context"
	"errors"
	"time"
)

// Spend is the current-period OCI infra spend the Cost bucket renders. It is a
// deliberately narrow projection of the usage-api response: total cost, currency,
// the period it covers, and a per-service breakdown. It carries no OCID, resource
// identifier or tenancy identifier, so a render or log built from it can never
// leak an OCI identifier.
type Spend struct {
	// Amount is the total computed cost across the period, in Currency.
	Amount float64
	// Currency is the ISO currency the amounts are denominated in (e.g. "USD").
	// It is not an identifier and is safe to show.
	Currency string
	// PeriodStart and PeriodEnd bound the window the spend covers (month-to-date).
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Lines is the per-service breakdown, largest first. Service names are not
	// identifiers.
	Lines []SpendLine
}

// SpendLine is one service's contribution to the period spend. Service is a
// service name (e.g. "COMPUTE"), never an OCID.
type SpendLine struct {
	Service string
	Amount  float64
}

// UsageClient is the read-only seam onto OCI's usage-api that the Cost bucket
// depends on. It exposes exactly one read and no mutating operation, so a bucket
// holding a UsageClient cannot modify infrastructure. A fake implements it in
// tests, which is the only way to exercise the bucket without a real OCI instance
// principal.
type UsageClient interface {
	// CurrentPeriodSpend returns the month-to-date infra spend. It never mutates
	// anything on OCI; an unreachable usage-api yields a SourceDownError so the
	// bucket degrades to its last-known value flagged stale.
	CurrentPeriodSpend(ctx context.Context) (Spend, error)
}

// SourceDownError reports that OCI's usage-api could not be reached or answered
// with an error. The Cost bucket branches on it to serve last-known spend flagged
// stale (the degrade-to-stale invariant). Its message names only the usage-api
// operation and wraps the transport error; it never embeds an OCI identifier.
type SourceDownError struct {
	// Err is the underlying error, already reduced to non-identifying facts by the
	// client: a usage-api service error is sanitised to its HTTP status and service
	// code before it reaches this field, so no free-form message, endpoint or
	// request id — the fields that could echo an OCI identifier — is carried here.
	Err error
}

func (e *SourceDownError) Error() string {
	return "oci: usage-api unreachable: " + e.Err.Error()
}

// Unwrap exposes the transport error to errors.Is/As.
func (e *SourceDownError) Unwrap() error { return e.Err }

// IsSourceDown reports whether err is (or wraps) a SourceDownError, i.e. the
// usage-api was unreachable. The bucket uses it to keep serving last-known spend.
func IsSourceDown(err error) bool {
	var sd *SourceDownError
	return errors.As(err, &sd)
}
