package oci

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

// fakeUsageAPI is a controllable stand-in for the OCI usage-api SDK client. A test
// sets the response and/or error it returns and inspects the request it received,
// exercising the SDK→Spend mapping and the degrade path with no OCI auth.
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
		// Identifiers the usage-api returns that must NOT survive into Spend.
		TenantId:      common.String("ocid1.tenancy.oc1..aaaaSECRET"),
		CompartmentId: common.String("ocid1.compartment.oc1..bbbbSECRET"),
		ResourceId:    common.String("ocid1.instance.oc1..ccccSECRET"),
	}
}

func newTestClient(f *fakeUsageAPI) *Client {
	return &Client{
		api:     f,
		tenancy: "ocid1.tenancy.oc1..aaaaSECRET",
		now:     func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) },
	}
}

// TestUsageAPISeamIsSingleReadOnlyMethod is the read-only invariant made a test:
// the SDK seam the client depends on exposes exactly one operation and it is the
// read RequestSummarizedUsages. No mutating usage-api call can reach the client
// because none is in the seam.
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

// TestCurrentPeriodSpendSummarizes proves the SDK response is projected into a
// Spend: totalled, grouped by service largest-first, with the currency carried.
func TestCurrentPeriodSpendSummarizes(t *testing.T) {
	f := &fakeUsageAPI{resp: usageapi.RequestSummarizedUsagesResponse{
		UsageAggregation: usageapi.UsageAggregation{Items: []usageapi.UsageSummary{
			item("COMPUTE", "USD", 10),
			item("STORAGE", "USD", 25),
			item("COMPUTE", "USD", 5), // same service, must fold into one line
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

// TestCurrentPeriodSpendReadsMonthlyCost proves the client asks the usage-api for
// month-to-date COST at monthly granularity — a read query, never a mutation.
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

// TestSpendCarriesNoOCIIdentifier proves the projection drops every OCI identifier:
// the usage-api items carry tenancy/compartment/resource OCIDs, but neither the
// Spend nor its formatted form contains an "ocid1." token. This is the no-leak
// invariant enforced at the mapping, where the identifiers are still present.
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
	// Also guard against a future field being added that carries an identifier.
	for _, f := range structFieldNames(spend) {
		lower := strings.ToLower(f)
		for _, banned := range []string{"ocid", "tenant", "compartment", "resourceid"} {
			if strings.Contains(lower, banned) {
				t.Errorf("Spend field %q looks like an identifier field (%q)", f, banned)
			}
		}
	}
}

// fakeServiceError implements the OCI common.ServiceError interface with a
// free-form message and request id that (hostilely) embed OCI identifiers, so a
// test can prove they are stripped before the error can reach a log.
type fakeServiceError struct{}

func (fakeServiceError) GetHTTPStatusCode() int { return 404 }
func (fakeServiceError) GetCode() string        { return "NotAuthorizedOrNotFound" }
func (fakeServiceError) GetMessage() string {
	return "not authorized for ocid1.tenancy.oc1..aaaaSECRET on ocid1.instance.oc1..ccccSECRET"
}
func (fakeServiceError) GetOpcRequestID() string { return "req-ocid1.request.oc1..dddd" }
func (fakeServiceError) Error() string           { return "Service error: " + fakeServiceError{}.GetMessage() }

// TestServiceErrorSanitisedBeforeLog proves the leakage-path defence: a usage-api
// service error whose message and request id carry OCI identifiers is reduced to
// just its HTTP status and service code, so the SourceDownError that reaches the
// bucket (and the shell's collect-failure log) contains no identifier.
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

// circuitBreakerOpenErr reproduces the plain error the OCI SDK returns when the
// usage-api client's default circuit breaker is open (common.getCircuitBreakerError):
// a non-service error that embeds the request endpoint and a history of the prior
// service failures — opc-request-id, error code and the free-form message, here
// carrying OCIDs. It is NOT a common.ServiceError, so it is the shape that would slip
// past the service-error branch of sanitize.
func circuitBreakerOpenErr() error {
	return fmt.Errorf(
		"circuit breaker is open, so this request was not sent to the Usageapi service.\n\n" +
			"URL which circuit breaker prevented request to - usageapi.us-ashburn-1.oci.oraclecloud.com/20200107/usage \n" +
			"Circuit Breaker Info \n Name - Usageapi \n State - open \n\n" +
			"Errors from Usageapi service which opened the circuit breaker:\n\n" +
			"Opc-Req-id - req-ocid1.request.oc1..dddd\nErrorCode - 404 - NotAuthorizedOrNotFound\n" +
			"ErrorMessage - not authorized for ocid1.tenancy.oc1..aaaaSECRET on ocid1.instance.oc1..ccccSECRET\n\n")
}

// TestCircuitBreakerErrorSanitisedBeforeLog is the epic-close regression guard: when
// a sustained usage-api outage trips the SDK's default circuit breaker, the open-
// breaker error embeds the endpoint, opc-request-id and OCIDs but is not a service
// error — so it must be redacted before it reaches the shell's collect-failure log.
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

// TestTransportErrorKeptVerbatim proves the redaction is targeted: a genuine
// transport/dial error carries no OCI identifier, so it is preserved verbatim for
// diagnostics rather than over-redacted.
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

// TestCurrentPeriodSpendDegradesOnError proves an unreachable usage-api surfaces as
// a SourceDownError so the bucket keeps last-known spend flagged stale.
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

// structFieldNames returns the field names of a struct value, for the identifier
// guard above.
func structFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	return names
}
