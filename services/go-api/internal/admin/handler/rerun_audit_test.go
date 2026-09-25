package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TestReRun_EmitsOperatorAuditRecord guards #999: an operator-triggered rerun
// re-issues real provider requests, so each successful rerun must leave a
// structured audit record of who ran it, what was rerun, and when — without
// copying the raw query text into logs (#1097).
func TestReRun_EmitsOperatorAuditRecord(t *testing.T) {
	const rawQuery = "zqxj private diagnosis clinic"

	okReRun := func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		return requeststore.ReRunResult{Query: rawQuery}, nil
	}
	okDetail := func(context.Context, string) (requeststore.DetailReRunResult, error) {
		return requeststore.DetailReRunResult{Query: rawQuery}, nil
	}
	okSearch := func(context.Context, string, []string) ([]requeststore.ResultRow, error) {
		return nil, nil
	}

	cases := []struct {
		name       string
		path       string
		body       string
		wantAction string
		wantKinds  []any
	}{
		{"rerun", "/rerun", `{"query":"` + rawQuery + `","kinds":["artist","track"]}`, "rerun", []any{"artist", "track"}},
		{"rerun-detail", "/rerun-detail", `{"query":"` + rawQuery + `","kinds":["artist"]}`, "rerun_detail", []any{}},
		{"test-search", "/search", `{"query":"` + rawQuery + `","kinds":["track"]}`, "test_search", []any{"track"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			operator := shared.NewUserId(uuid.New())
			h := New(nil, nil).WithReRunner(okReRun).WithDetailReRunner(okDetail).WithSearchInspector(okSearch)

			r := chi.NewRouter()
			r.Use(httputil.CorrelationID)
			r.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					next.ServeHTTP(w, req.WithContext(auth.ContextWithUserID(req.Context(), operator)))
				})
			})
			r.Use(OperatorOnly(operator.String()))
			h.RegisterData(r)

			before := time.Now().UTC().Add(-time.Second)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-Correlation-ID", "corr-999")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}

			logged := buf.String()
			if strings.Contains(logged, "diagnosis") || strings.Contains(logged, "zqxj") {
				t.Fatalf("audit log leaked raw query text:\n%s", logged)
			}

			var audit map[string]any
			for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
				if line == "" {
					continue
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(line), &m); err != nil {
					t.Fatalf("non-JSON log line %q: %v", line, err)
				}
				if m["msg"] == "admin.operator_action" {
					if audit != nil {
						t.Fatalf("audit record emitted more than once:\n%s", logged)
					}
					audit = m
				}
			}
			if audit == nil {
				t.Fatalf("no admin.operator_action audit record emitted; logs:\n%s", logged)
			}

			if audit["action"] != tc.wantAction {
				t.Errorf("action = %v, want %q", audit["action"], tc.wantAction)
			}
			if audit["actor"] != operator.String() {
				t.Errorf("actor = %v, want %q", audit["actor"], operator.String())
			}
			if audit["corr_id"] != "corr-999" {
				t.Errorf("corr_id = %v, want corr-999", audit["corr_id"])
			}
			st, ok := audit["search_text"].(map[string]any)
			if !ok || st["len"] != float64(len([]rune(rawQuery))) || st["fp"] == "" || st["fp"] == nil {
				t.Errorf("search_text = %v, want len %d and a fingerprint", audit["search_text"], len([]rune(rawQuery)))
			}
			kinds, ok := audit["kinds"].([]any)
			if !ok || len(kinds) != len(tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v", audit["kinds"], tc.wantKinds)
			}
			for i := range kinds {
				if kinds[i] != tc.wantKinds[i] {
					t.Errorf("kinds[%d] = %v, want %v", i, kinds[i], tc.wantKinds[i])
				}
			}
			atStr, _ := audit["at"].(string)
			at, err := time.Parse(time.RFC3339Nano, atStr)
			if err != nil {
				t.Fatalf("at = %v: %v", audit["at"], err)
			}
			if at.Before(before) || at.After(time.Now().UTC().Add(time.Second)) {
				t.Errorf("at = %v, not within the request window", at)
			}
		})
	}
}
