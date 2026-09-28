package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

type usageAPI interface {
	RequestSummarizedUsages(ctx context.Context, request usageapi.RequestSummarizedUsagesRequest) (usageapi.RequestSummarizedUsagesResponse, error)
}

type Client struct {
	api     usageAPI
	tenancy string
	now     func() time.Time
}

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

func sanitize(err error) error {
	if se, ok := common.IsServiceError(err); ok {
		return describeServiceError(se)
	}
	if common.IsCircuitBreakerError(err) || carriesOCIIdentifier(err.Error()) {
		return errors.New("usage-api unavailable (repeated failures; identifying details redacted)")
	}
	return err
}

func describeServiceError(se common.ServiceError) error {
	status, code := se.GetHTTPStatusCode(), se.GetCode()
	if isAuthorizationDenied(status, code) {
		return fmt.Errorf(
			"usage-api denied access (HTTP %d %s): the instance principal lacks usage-api read — grant 'endorse dynamic-group <overseer-dg> to read usage-report in tenancy usage-report'",
			status, code)
	}
	return fmt.Errorf("usage-api returned HTTP %d (%s)", status, code)
}

func isAuthorizationDenied(status int, code string) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	case http.StatusNotFound:
		return strings.Contains(code, "NotAuthorized")
	default:
		return false
	}
}

func carriesOCIIdentifier(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "ocid1.") ||
		strings.Contains(lower, "opc-request-id") ||
		strings.Contains(lower, "opc-req-id")
}

func monthToDate(now time.Time) (start, end time.Time) {
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	return start, end
}

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

func serviceName(s *string) string {
	if v := deref(s); v != "" {
		return v
	}
	return "other"
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
