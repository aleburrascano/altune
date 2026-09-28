package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

type fakeUsageAPI struct {
	resp usageapi.RequestSummarizedUsagesResponse
	err  error
	got  usageapi.RequestSummarizedUsagesRequest
}

func (f *fakeUsageAPI) RequestSummarizedUsages(_ context.Context, req usageapi.RequestSummarizedUsagesRequest) (usageapi.RequestSummarizedUsagesResponse, error) {
	f.got = req
	return f.resp, f.err
}

func item(service, currency string, amount float32) usageapi.UsageSummary {
	return usageapi.UsageSummary{
		Service:        common.String(service),
		Currency:       common.String(currency),
		ComputedAmount: common.Float32(amount),
		TenantId:       common.String("ocid1.tenancy.oc1..aaaaSECRET"),
		CompartmentId:  common.String("ocid1.compartment.oc1..bbbbSECRET"),
		ResourceId:     common.String("ocid1.instance.oc1..ccccSECRET"),
	}
}

func newTestClient(f *fakeUsageAPI) *Client {
	return &Client{
		api:     f,
		tenancy: "ocid1.tenancy.oc1..aaaaSECRET",
		now:     func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) },
	}
}

func TestUsageAPISeamIsSingleReadOnlyMethod(t *testing.T) {
	typ := reflect.TypeOf((*usageAPI)(nil)).Elem()
	if got := typ.NumMethod(); got != 1 {
		t.Fatalf("usageAPI seam exposes %d methods, want exactly 1 (single read-only surface)", got)
	}
	name := typ.Method(0).Name
	if name != "RequestSummarizedUsages" {
		t.Errorf("usageAPI method = %q, want RequestSummarizedUsages", name)
	}
	for _, verb := range []string{"Create", "Update", "Delete", "Put", "Post", "Set", "Modify", "Schedule", "Remove"} {
		if strings.Contains(name, verb) {
			t.Errorf("usageAPI method %q contains mutating verb %q", name, verb)
		}
	}
}

func TestCurrentPeriodSpendSummarizes(t *testing.T) {
	f := &fakeUsageAPI{resp: usageapi.RequestSummarizedUsagesResponse{
		UsageAggregation: usageapi.UsageAggregation{Items: []usageapi.UsageSummary{
			item("COMPUTE", "USD", 10),
			item("STORAGE", "USD", 25),
			item("COMPUTE", "USD", 5),
		}},
	}}
	spend, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if err != nil {
		t.Fatalf("CurrentPeriodSpend = %v, want nil", err)
	}
	if spend.Amount != 40 {
		t.Errorf("total = %v, want 40", spend.Amount)
	}
	if spend.Currency != "USD" {
		t.Errorf("currency = %q, want USD", spend.Currency)
	}
	if len(spend.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (folded by service)", len(spend.Lines))
	}
	if spend.Lines[0].Service != "STORAGE" || spend.Lines[0].Amount != 25 {
		t.Errorf("first line = %+v, want STORAGE 25 (largest first)", spend.Lines[0])
	}
	if spend.Lines[1].Service != "COMPUTE" || spend.Lines[1].Amount != 15 {
		t.Errorf("second line = %+v, want COMPUTE 15 (folded)", spend.Lines[1])
	}
}

func TestCurrentPeriodSpendReadsMonthlyCost(t *testing.T) {
	f := &fakeUsageAPI{}
	if _, err := newTestClient(f).CurrentPeriodSpend(context.Background()); err != nil {
		t.Fatalf("CurrentPeriodSpend = %v", err)
	}
	d := f.got.RequestSummarizedUsagesDetails
	if d.Granularity != usageapi.RequestSummarizedUsagesDetailsGranularityMonthly {
		t.Errorf("granularity = %q, want MONTHLY", d.Granularity)
	}
	if d.QueryType != usageapi.RequestSummarizedUsagesDetailsQueryTypeCost {
		t.Errorf("queryType = %q, want COST", d.QueryType)
	}
	if d.TimeUsageStarted == nil || d.TimeUsageStarted.Day() != 1 {
		t.Errorf("start = %v, want the first of the month", d.TimeUsageStarted)
	}
	if d.TimeUsageEnded == nil || !d.TimeUsageEnded.After(d.TimeUsageStarted.Time) {
		t.Errorf("end %v must be after start %v", d.TimeUsageEnded, d.TimeUsageStarted)
	}
}

func TestSpendCarriesNoOCIIdentifier(t *testing.T) {
	f := &fakeUsageAPI{resp: usageapi.RequestSummarizedUsagesResponse{
		UsageAggregation: usageapi.UsageAggregation{Items: []usageapi.UsageSummary{
			item("COMPUTE", "USD", 10),
		}},
	}}
	spend, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if err != nil {
		t.Fatalf("CurrentPeriodSpend = %v", err)
	}
	if dump := fmt.Sprintf("%+v", spend); strings.Contains(dump, "ocid1.") {
		t.Errorf("Spend leaked an OCI identifier:\n%s", dump)
	}
	for _, f := range structFieldNames(spend) {
		lower := strings.ToLower(f)
		for _, banned := range []string{"ocid", "tenant", "compartment", "resourceid"} {
			if strings.Contains(lower, banned) {
				t.Errorf("Spend field %q looks like an identifier field (%q)", f, banned)
			}
		}
	}
}

func TestSpendSerializesCamelCase(t *testing.T) {
	blob, err := json.Marshal(Spend{
		Amount:   41.5,
		Currency: "USD",
		Lines:    []SpendLine{{Service: "COMPUTE", Amount: 16.5}},
	})
	if err != nil {
		t.Fatalf("marshal spend: %v", err)
	}
	got := string(blob)
	for _, key := range []string{`"amount"`, `"currency"`, `"periodStart"`, `"periodEnd"`, `"lines"`, `"service"`} {
		if !strings.Contains(got, key) {
			t.Errorf("serialized Spend missing camelCase key %s:\n%s", key, got)
		}
	}
	for _, pascal := range []string{`"Amount"`, `"Currency"`, `"PeriodStart"`, `"PeriodEnd"`, `"Lines"`, `"Service"`} {
		if strings.Contains(got, pascal) {
			t.Errorf("serialized Spend leaked PascalCase key %s (old untagged shape):\n%s", pascal, got)
		}
	}
}

type fakeServiceError struct{}

func (fakeServiceError) GetHTTPStatusCode() int { return 404 }
func (fakeServiceError) GetCode() string        { return "NotAuthorizedOrNotFound" }
func (fakeServiceError) GetMessage() string {
	return "not authorized for ocid1.tenancy.oc1..aaaaSECRET on ocid1.instance.oc1..ccccSECRET"
}
func (fakeServiceError) GetOpcRequestID() string { return "req-ocid1.request.oc1..dddd" }
func (fakeServiceError) Error() string           { return "Service error: " + fakeServiceError{}.GetMessage() }

func TestServiceErrorSanitisedBeforeLog(t *testing.T) {
	f := &fakeUsageAPI{err: fakeServiceError{}}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if !IsSourceDown(err) {
		t.Fatalf("error = %v, want source-down", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "ocid1.") {
		t.Errorf("logged error leaked an OCI identifier: %q", msg)
	}
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "NotAuthorizedOrNotFound") {
		t.Errorf("logged error = %q, want the non-identifying status and code", msg)
	}
}

func TestNotAuthorizedIsNamedAsPolicyGap(t *testing.T) {
	f := &fakeUsageAPI{err: fakeServiceError{}}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if !IsSourceDown(err) {
		t.Fatalf("error = %v, want source-down", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "ocid1.") {
		t.Errorf("authorization error leaked an OCI identifier: %q", msg)
	}
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "NotAuthorizedOrNotFound") {
		t.Errorf("authorization error = %q, want the status and code retained", msg)
	}
	for _, want := range []string{"denied", "usage-report"} {
		if !strings.Contains(msg, want) {
			t.Errorf("authorization error = %q, want it to name the policy gap (%q)", msg, want)
		}
	}
	if strings.Contains(msg, "returned HTTP 404") {
		t.Errorf("authorization error still reads as a bare unrouted 404: %q", msg)
	}
}

func TestServiceErrorRetainsStatusForNonAuthFailure(t *testing.T) {
	f := &fakeUsageAPI{err: serviceErrorAt(503, "InternalServerError")}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if !IsSourceDown(err) {
		t.Fatalf("error = %v, want source-down", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "503") || !strings.Contains(msg, "InternalServerError") {
		t.Errorf("5xx error = %q, want the status and code", msg)
	}
	if strings.Contains(msg, "usage-report") || strings.Contains(msg, "denied") {
		t.Errorf("5xx error misattributed to a policy gap: %q", msg)
	}
}

type codedServiceError struct {
	status int
	code   string
}

func (e codedServiceError) GetHTTPStatusCode() int  { return e.status }
func (e codedServiceError) GetCode() string         { return e.code }
func (e codedServiceError) GetMessage() string      { return e.code }
func (e codedServiceError) GetOpcRequestID() string { return "" }
func (e codedServiceError) Error() string           { return e.code }

func serviceErrorAt(status int, code string) error {
	return codedServiceError{status: status, code: code}
}

func circuitBreakerOpenErr() error {
	return fmt.Errorf(
		"circuit breaker is open, so this request was not sent to the Usageapi service.\n\n" +
			"URL which circuit breaker prevented request to - usageapi.us-ashburn-1.oci.oraclecloud.com/20200107/usage \n" +
			"Circuit Breaker Info \n Name - Usageapi \n State - open \n\n" +
			"Errors from Usageapi service which opened the circuit breaker:\n\n" +
			"Opc-Req-id - req-ocid1.request.oc1..dddd\nErrorCode - 404 - NotAuthorizedOrNotFound\n" +
			"ErrorMessage - not authorized for ocid1.tenancy.oc1..aaaaSECRET on ocid1.instance.oc1..ccccSECRET\n\n")
}

func TestCircuitBreakerErrorSanitisedBeforeLog(t *testing.T) {
	f := &fakeUsageAPI{err: circuitBreakerOpenErr()}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if !IsSourceDown(err) {
		t.Fatalf("error = %v, want source-down", err)
	}
	msg := err.Error()
	for _, leak := range []string{"ocid1.", "Opc-Req-id", "opc-request-id", "usageapi.us-ashburn-1", "20200107"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(leak)) {
			t.Errorf("open-breaker error leaked %q into the log path: %q", leak, msg)
		}
	}
	if !strings.Contains(msg, "redacted") {
		t.Errorf("open-breaker error not redacted: %q", msg)
	}
}

func TestTransportErrorKeptVerbatim(t *testing.T) {
	f := &fakeUsageAPI{err: errors.New("dial tcp 169.254.169.254:443: connect: connection refused")}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if !IsSourceDown(err) {
		t.Fatalf("error = %v, want source-down", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("transport error wrongly redacted: %q", err.Error())
	}
}

func TestCurrentPeriodSpendDegradesOnError(t *testing.T) {
	f := &fakeUsageAPI{err: errors.New("dial 169.254.169.254: connection refused")}
	_, err := newTestClient(f).CurrentPeriodSpend(context.Background())
	if err == nil {
		t.Fatal("CurrentPeriodSpend with usage-api down returned nil error")
	}
	if !IsSourceDown(err) {
		t.Errorf("error is not source-down: %v", err)
	}
}

func structFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	return names
}
