package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsStrayClosingByte(t *testing.T) {
	for _, body := range []string{`{"a":1}}`, `{"a":1}]`, `{"a":1} {"a":2}`} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		rec := httptest.NewRecorder()
		var dst struct{ A int }
		if DecodeJSON(rec, req, &dst) || rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: want rejected with 400, got code %d", body, rec.Code)
		}
	}
}
