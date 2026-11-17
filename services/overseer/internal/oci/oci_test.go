package oci_test

import (
	"altune/overseer/internal/oci"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSpendSerializesCamelCase(t *testing.T) {
	blob, err := json.Marshal(oci.Spend{
		Amount:   41.5,
		Currency: "USD",
		Lines:    []oci.SpendLine{{Service: "COMPUTE", Amount: 16.5}},
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

func TestSourceDownErrorNamesTheUsageAPIAndKeepsItsCause(t *testing.T) {
	cause := errors.New("dial refused")
	err := fmt.Errorf("read spend: %w", &oci.SourceDownError{Err: cause})

	var down *oci.SourceDownError
	if !errors.As(err, &down) || down.Error() != "oci: usage-api unavailable: dial refused" {
		t.Fatalf("source-down error = %v, want the usage-api prefix and its cause", err)
	}
	if !errors.Is(err, cause) {
		t.Error("source-down error does not unwrap to its cause")
	}
	if !oci.IsSourceDown(err) {
		t.Error("IsSourceDown = false for a wrapped SourceDownError, want true")
	}
	if oci.IsSourceDown(cause) {
		t.Error("IsSourceDown = true for a plain error, want false")
	}
}
