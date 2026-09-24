package persistence

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type erroringQuerier struct {
	err error
}

func (f erroringQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f erroringQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: f.err}
}

func failingRepo(err error) *PgxQueueStateRepository {
	return &PgxQueueStateRepository{pool: erroringQuerier{err: err}, metrics: ports.NoopQueueStateMetrics()}
}

func TestDeleteForUser_DeadlineExceeded_IsRetryable503(t *testing.T) {
	err := failingRepo(context.DeadlineExceeded).DeleteForUser(context.Background(), testUser())

	if !errors.Is(err, ports.ErrQueueStateUnavailable) {
		t.Fatalf("err = %v, want ErrQueueStateUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cause lost from the chain: %v", err)
	}
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodDelete, "/queue-state", nil), err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if !strings.Contains(rec.Body.String(), `"code":"playback.unavailable"`) {
		t.Errorf("body = %q, want code playback.unavailable", rec.Body.String())
	}
}

func TestQueueStateOp_TransientClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"connection refused", &pgconn.ConnectError{}, true},
		{"admin shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"constraint violation", &pgconn.PgError{Code: "23505"}, false},
		{"client cancel", context.Canceled, false},
		{"no rows", pgx.ErrNoRows, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := failingRepo(tc.err).DeleteForUser(context.Background(), testUser())
			if got := errors.Is(err, ports.ErrQueueStateUnavailable); got != tc.want {
				t.Fatalf("unavailable = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestGetForUser_TransientFailureIsNotCorruptState(t *testing.T) {
	_, err := failingRepo(context.DeadlineExceeded).GetForUser(context.Background(), testUser())

	if errors.Is(err, ports.ErrCorruptStoredState) || !errors.Is(err, ports.ErrQueueStateUnavailable) {
		t.Fatalf("err = %v, want unavailable and not corrupt", err)
	}
}
