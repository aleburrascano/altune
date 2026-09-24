package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeThroughCap(body string, limit int64) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var dst map[string]any
	h := MaxBodySize(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		DecodeJSON(w, r, &dst)
	}))
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	return rec
}

func TestDecodeJSONLimits(t *testing.T) {
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"oversize body", `{"a":"` + strings.Repeat("x", 64) + `"}`, "request.too_large", http.StatusRequestEntityTooLarge},
		{"trailing data", `{"a":1}garbage`, "request.invalid_body", http.StatusBadRequest},
		{"malformed", `{bad`, "request.invalid_body", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := decodeThroughCap(tc.body, 16)
			if rec.Code != tc.status {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.status)
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body: got %s, want code %s", rec.Body.String(), tc.code)
			}
		})
	}
}
