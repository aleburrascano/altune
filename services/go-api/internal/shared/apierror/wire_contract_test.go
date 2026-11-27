package apierror_test

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"testing"
)

func TestWire_TheCatalogListsEveryCodeTheClientMayReceive(t *testing.T) {
	catalog := apierror.Catalog()
	names := make([]string, 0, len(catalog))
	for _, c := range catalog {
		names = append(names, c.String())
	}
	body, err := json.Marshal(names)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}

	jsoncontract.AssertWireExact(t, "api_error_codes", body)
}
