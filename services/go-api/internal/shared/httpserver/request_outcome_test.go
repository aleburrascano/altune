package httpserver_test

import (
	"altune/go-api/internal/shared/httpserver"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func recordingObserver(seen *[]httpserver.Outcome) httpserver.Observer {
	return func(r *http.Request) (*http.Request, func(httpserver.Outcome)) {
		return r, func(o httpserver.Outcome) { *seen = append(*seen, o) }
	}
}

func TestObserveRequests_EveryObserverReadsTheSameOutcomeFromOneMeasurement(t *testing.T) {
	var first, second []httpserver.Outcome
	reads := 0
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time {
		reads++
		return base.Add(time.Duration(reads-1) * 250 * time.Millisecond)
	}
	router := chi.NewRouter()
	router.Use(httpserver.ObserveRequests(now, recordingObserver(&first), recordingObserver(&second)))
	router.Get("/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
		httpserver.RecordRequestUser(r.Context(), "user-9")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("abc"))
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracks/7", nil))

	want := httpserver.Outcome{
		Method: http.MethodGet, Path: "/tracks/7", Route: "/tracks/{id}",
		Status: http.StatusTeapot, Bytes: 3, Duration: 250 * time.Millisecond, UserID: "user-9",
	}
	if len(first) != 1 || len(second) != 1 || first[0] != want || second[0] != want {
		t.Fatalf("outcomes = %+v and %+v, want one identical %+v each", first, second, want)
	}
	if reads != 2 {
		t.Fatalf("clock read %d times, want 2 (start and end of one measurement)", reads)
	}
}

func TestObserveRequests_FinishersRunInReverseOrderAndAPanickingOneDoesNotStopTheRest(t *testing.T) {
	var order []string
	observer := func(name string, boom bool) httpserver.Observer {
		return func(r *http.Request) (*http.Request, func(httpserver.Outcome)) {
			return r, func(httpserver.Outcome) {
				order = append(order, name)
				if boom {
					panic("observer blew up")
				}
			}
		}
	}
	handler := httpserver.ObserveRequests(time.Now, observer("outer", false), observer("middle", true), observer("inner", false))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the response untouched by an observer panic", rec.Code)
	}
	if got := order; len(got) != 3 || got[0] != "inner" || got[1] != "middle" || got[2] != "outer" {
		t.Fatalf("finish order = %v, want inner, middle, outer", got)
	}
}

func TestObserveRequests_AnObserverMayDeclineARequest(t *testing.T) {
	declining := func(r *http.Request) (*http.Request, func(httpserver.Outcome)) { return r, nil }
	handler := httpserver.ObserveRequests(time.Now, declining)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestObserveRequests_RefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ObserveRequests(nil) did not panic, want a refusal at construction")
		}
	}()
	httpserver.ObserveRequests(nil)
}
