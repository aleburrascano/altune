package oci

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

// usageAPI is the minimal read surface of the OCI usage-api SDK client the Client
// depends on: exactly one operation, the read RequestSummarizedUsages. Narrowing
// the SDK to this interface is the enforcement point for the read-only invariant —
// the Client cannot reach any mutating usage-api call because none is in the seam —
// and it lets a fake drive the SDK→Spend mapping with no OCI auth.
type usageAPI interface {
	RequestSummarizedUsages(ctx context.Context, request usageapi.RequestSummarizedUsagesRequest) (usageapi.RequestSummarizedUsagesResponse, error)
}

// Client is the read-only OCI usage-api client. It authenticates by instance
// principal (no stored key), reads current-period cost through the usage-api, and
// holds no mutating operation. It satisfies UsageClient.
type Client struct {
	api     usageAPI
	tenancy string
	now     func() time.Time
}

// NewFromInstancePrincipal builds the read-only usage-api client authenticated by
// instance principal — no stored key ever touches disk or config. The tenancy
// OCID is read from the same instance-principal provider, so no OCID is configured
// or logged by hand. This only succeeds on an OCI instance whose IAM policy grants
// it usage-api read; off an instance the metadata handshake fails and the caller
// degrades to stale.
func NewFromInstancePrincipal() (*Client, error) {
	provider, err := auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		return nil, fmt.Errorf("oci: instance principal unavailable: %w", err)
	}
	api, err := usageapi.NewUsageapiClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("oci: usage-api client: %w", err)
	}
	tenancy, err := provider.TenancyOCID()
	if err != nil {
		return nil, fmt.Errorf("oci: tenancy unavailable: %w", err)
	}
	return &Client{api: api, tenancy: tenancy, now: time.Now}, nil
}

// CurrentPeriodSpend reads the month-to-date cost from the usage-api and projects
// it into a Spend that carries no OCI identifier. It is a pure read: it issues one
// RequestSummarizedUsages and mutates nothing. An error from the usage-api is
// wrapped as SourceDownError so the bucket degrades to last-known flagged stale.
func (c *Client) CurrentPeriodSpend(ctx context.Context) (Spend, error) {
	start, end := monthToDate(c.now().UTC())
	req := usageapi.RequestSummarizedUsagesRequest{
		RequestSummarizedUsagesDetails: usageapi.RequestSummarizedUsagesDetails{
			TenantId:          common.String(c.tenancy),
			TimeUsageStarted:  &common.SDKTime{Time: start},
			TimeUsageEnded:    &common.SDKTime{Time: end},
			Granularity:       usageapi.RequestSummarizedUsagesDetailsGranularityMonthly,
			QueryType:         usageapi.RequestSummarizedUsagesDetailsQueryTypeCost,
			GroupBy:           []string{"service"},
			IsAggregateByTime: common.Bool(false),
		},
	}
	resp, err := c.api.RequestSummarizedUsages(ctx, req)
	if err != nil {
		return Spend{}, &SourceDownError{Err: sanitize(err)}
	}
	return summarize(resp.UsageAggregation, start, end), nil
}

// sanitize reduces a usage-api error to non-identifying facts before it can reach
// a log. An OCI service error is rendered as just its HTTP status and service
// error code (e.g. "HTTP 404 (NotAuthorizedOrNotFound)") — never the free-form
// message, request endpoint or opc-request-id, which in some error shapes can echo
// a tenancy or resource identifier. A non-service (transport/dial) error carries no
// OCI identifier and is kept verbatim for diagnostics.
func sanitize(err error) error {
	if se, ok := common.IsServiceError(err); ok {
		return fmt.Errorf("usage-api returned HTTP %d (%s)", se.GetHTTPStatusCode(), se.GetCode())
	}
	return err
}

// monthToDate is the current calendar month so far, in UTC: from the first of the
// month to the start of tomorrow (the usage-api requires end strictly after
// start, and rejects a sub-day window on monthly granularity).
func monthToDate(now time.Time) (start, end time.Time) {
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	return start, end
}

// summarize projects the usage-api aggregation into a Spend. It reads only the
// computed amount, the currency and the service name from each item — never an
// OCID, resource id or tenancy field — so an identifier cannot survive into the
// rendered panel. Lines are returned largest first.
func summarize(agg usageapi.UsageAggregation, start, end time.Time) Spend {
	perService := map[string]float64{}
	var total float64
	var currency string
	for _, it := range agg.Items {
		amount := float64(deref(it.ComputedAmount))
		total += amount
		perService[serviceName(it.Service)] += amount
		if currency == "" {
			currency = deref(it.Currency)
		}
	}
	lines := make([]SpendLine, 0, len(perService))
	for svc, amt := range perService {
		lines = append(lines, SpendLine{Service: svc, Amount: amt})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Amount != lines[j].Amount {
			return lines[i].Amount > lines[j].Amount
		}
		return lines[i].Service < lines[j].Service
	})
	return Spend{Amount: total, Currency: currency, PeriodStart: start, PeriodEnd: end, Lines: lines}
}

// serviceName is the item's service label, or a fixed placeholder when the
// usage-api omits it. It is a service name, never an identifier.
func serviceName(s *string) string {
	if v := deref(s); v != "" {
		return v
	}
	return "other"
}

// deref reads a value behind an optional SDK pointer, yielding the zero value when
// the field is absent.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
