package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverer_CatchesPanic_Returns500(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went very wrong")
	}))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Detail != "internal server error" {
		t.Errorf("detail = %q, want %q", body.Detail, "internal server error")
	}
}

func TestRecoverer_NoPanic_PassesThrough(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fine"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fine" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fine")
	}
}

type headerCountingRecorder struct {
	*httptest.ResponseRecorder
	writeHeaderCalls int
}

func (w *headerCountingRecorder) WriteHeader(code int) {
	w.writeHeaderCalls++
	w.ResponseRecorder.WriteHeader(code)
}

func TestRecoverer_RepanicsErrAbortHandler(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	rec := httptest.NewRecorder()

	defer func() {
		got, _ := recover().(error)
		if !errors.Is(got, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want http.ErrAbortHandler re-panicked", got)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want no error body on abort", rec.Body.String())
		}
	}()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/abort", nil))
}

func TestRecoverer_PanicAfterHeadersSentKeepsTheSentResponse(t *testing.T) {
	tests := []struct {
		name  string
		start func(http.ResponseWriter)
	}{
		{"explicit WriteHeader", func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }},
		{"implicit header from Write", func(w http.ResponseWriter) { _, _ = w.Write([]byte("partial")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tt.start(w)
				panic("mid-stream failure")
			}))
			rec := &headerCountingRecorder{ResponseRecorder: httptest.NewRecorder()}

			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

			if rec.Code == http.StatusInternalServerError {
				t.Errorf("status = 500, want the status already sent")
			}
			if rec.writeHeaderCalls > 1 {
				t.Errorf("WriteHeader called %d times, want at most once", rec.writeHeaderCalls)
			}
			if got := rec.Body.String(); got != "" && got != "partial" {
				t.Errorf("body = %q, want only what the handler wrote", got)
			}
		})
	}
}

func TestRequestLogger_OutsideRecovererLogsPanicAs500Error(t *testing.T) {
	ring := captureLogs(t)
	handler := RequestLogger(Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))

	complete := onlyRecord(t, ring, "request.complete")
	if complete.Attrs["status"] != "500" || complete.Level != "ERROR" {
		t.Errorf("request.complete = %s status=%s, want ERROR status=500", complete.Level, complete.Attrs["status"])
	}
}

func TestRecoverer_PanicAfterFlushKeepsTheImplicit200(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		panic("mid-stream failure")
	}))
	rec := &headerCountingRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sse", nil))

	if rec.writeHeaderCalls > 1 {
		t.Errorf("WriteHeader called %d times, want at most once", rec.writeHeaderCalls)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("body = %q, want no error body appended to the stream", got)
	}
}
