package httpserver_test

import (
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRequestLogger_DoesNotPanic(t *testing.T) {
	handler := httpserver.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequestLogger_LogsTheRoutePatternAndTheUserTheHandlerRecorded(t *testing.T) {
	ring := captureLogs(t)
	router := chi.NewRouter()
	router.Use(httpserver.RequestLogger)
	router.Get("/tracks/{trackId}", func(w http.ResponseWriter, r *http.Request) {
		httpserver.RecordRequestUser(r.Context(), "user-7")
		w.WriteHeader(http.StatusNoContent)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracks/abc-123", nil))

	complete := onlyRecord(t, ring, "request.complete")
	if complete.Attrs["route"] != "/tracks/{trackId}" || complete.Attrs["path"] != "/tracks/abc-123" || complete.Attrs["user_id"] != "user-7" {
		t.Fatalf("request.complete attrs = %v, want route /tracks/{trackId}, path /tracks/abc-123, user_id user-7", complete.Attrs)
	}
}

func TestRequestLogger_AnUnmatchedRouteIsOneBucketAndAnAnonymousRequestHasNoUser(t *testing.T) {
	ring := captureLogs(t)
	router := chi.NewRouter()
	router.Use(httpserver.RequestLogger)
	router.Get("/health", func(http.ResponseWriter, *http.Request) {})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no/such/route", nil))

	complete := onlyRecord(t, ring, "request.complete")
	if complete.Attrs["route"] != "unmatched" {
		t.Errorf("route = %q, want unmatched so scanners cannot mint a label per path", complete.Attrs["route"])
	}
	if _, present := complete.Attrs["user_id"]; present {
		t.Errorf("user_id = %q on a request nobody authenticated, want the key absent", complete.Attrs["user_id"])
	}
}

func TestRecordRequestUser_OutsideARequestLoggerIsANoOp(t *testing.T) {
	httpserver.RecordRequestUser(context.Background(), "user-7")
}

func TestRequestLogger_TracksStatusCode(t *testing.T) {
	handler := httpserver.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRequestLogger_RawQueryAbsentFromRing(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug")

	const secret = "queenssecretobsessionquery"
	handler := httpserver.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery/search?q="+secret+"&limit=5", nil)

	handler.ServeHTTP(httptest.NewRecorder(), req)

	sawRequestComplete := false
	for _, rec := range ring.Snapshot() {
		if rec.Message == "request.complete" {
			sawRequestComplete = true
		}
		if strings.Contains(rec.Message, secret) {
			t.Errorf("raw query leaked into ring message: %q", rec.Message)
		}
		for k, v := range rec.Attrs {
			if strings.Contains(v, secret) {
				t.Errorf("raw query leaked into ring attr %q = %q", k, v)
			}
		}
	}
	if !sawRequestComplete {
		t.Fatal("expected request.complete to be captured, so the redaction assertion is meaningful")
	}
}

func completedRequest(t *testing.T, h http.HandlerFunc) map[string]any {
	t.Helper()
	var out bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))

	httpserver.RequestLogger(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/probe", nil))

	dec := json.NewDecoder(&out)
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		if rec["msg"] == "request.complete" {
			return rec
		}
	}
	t.Fatal("no request.complete line was logged")
	return nil
}

func TestRequestLogger_LogsStatus200AndBytesWhenTheHandlerOnlyWrites(t *testing.T) {
	rec := completedRequest(t, func(w http.ResponseWriter, _ *http.Request) {
		if n, err := w.Write([]byte("hello")); n != 5 || err != nil {
			t.Errorf("Write = %d, %v; want 5, nil", n, err)
		}
	})
	if rec["status"] != float64(http.StatusOK) || rec["bytes"] != float64(5) || rec["level"] != "INFO" {
		t.Errorf("status, bytes, level = %v, %v, %v; want 200, 5, INFO", rec["status"], rec["bytes"], rec["level"])
	}
}

func TestRequestLogger_LogsTheWrittenStatusAndEveryByte(t *testing.T) {
	rec := completedRequest(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("abc"))
		_, _ = w.Write([]byte("de"))
	})
	if rec["status"] != float64(http.StatusCreated) || rec["bytes"] != float64(5) {
		t.Errorf("status, bytes = %v, %v; want 201, 5 (3+2)", rec["status"], rec["bytes"])
	}
}

func TestRequestLogger_LogsEveryOutcomeOnceAtInfoAndLeavesFaultsToTheBoundary(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusBadGateway} {
		rec := completedRequest(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		if rec["level"] != "INFO" || rec["status"] != float64(status) {
			t.Errorf("status %d logged at %v with status %v, want INFO carrying the status", status, rec["level"], rec["status"])
		}
	}
}

func TestRequestLogger_WritesOneLinePerRequest(t *testing.T) {
	ring := captureLogs(t)

	httpserver.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/probe", nil))

	if got := ring.Snapshot(); len(got) != 1 || got[0].Message != "request.complete" {
		t.Errorf("records = %+v, want only request.complete", got)
	}
}
