package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestOversizedKindsRejectedBeforeAudit(t *testing.T) {
	okReRun := func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		return requeststore.ReRunResult{}, nil
	}
	okSearch := func(context.Context, string, []string) ([]requeststore.ResultRow, error) {
		return nil, nil
	}
	body := `{"query":"a","kinds":["` + strings.Repeat(`x","`, 10000) + `x"]}`
	for _, path := range []string{"/rerun", "/search"} {
		t.Run(path, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			r := chi.NewRouter()
			New(nil, nil).WithReRunner(okReRun).WithSearchInspector(okSearch).RegisterData(r)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if rec.Body.Len() > 512 {
				t.Fatalf("400 body is %d bytes, want bounded", rec.Body.Len())
			}
			if strings.Contains(buf.String(), "admin.operator_action") {
				t.Fatalf("audit record written for rejected request: %s", buf.String())
			}
		})
	}
}
