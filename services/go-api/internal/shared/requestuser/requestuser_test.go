package requestuser_test

import (
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUserIDFromContext(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := ids.NewUserId(uuid.New())
		ctx := requestuser.ContextWithUserID(context.Background(), uid)

		got, ok := requestuser.UserIDFromContext(ctx)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
	})

	t.Run("absent", func(t *testing.T) {
		got, ok := requestuser.UserIDFromContext(context.Background())
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
	})
}

func TestTokenExpiryFromContext(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		want := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

		got, ok := requestuser.TokenExpiryFromContext(requestuser.ContextWithTokenExpiry(context.Background(), want))

		if !ok || !got.Equal(want) {
			t.Fatalf("got %v, %v; want %v, true", got, ok, want)
		}
	})

	t.Run("absent", func(t *testing.T) {
		got, ok := requestuser.TokenExpiryFromContext(context.Background())

		if ok || !got.IsZero() {
			t.Fatalf("got %v, %v; want zero time, false", got, ok)
		}
	})
}

func TestUntilTokenExpiry_EndsAtExpiryWithCause(t *testing.T) {
	parent := requestuser.ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := requestuser.UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if cause := context.Cause(ctx); !errors.Is(cause, requestuser.ErrTokenExpired) {
		t.Fatalf("cause = %v, want requestuser.ErrTokenExpired", cause)
	}
}

func TestUntilTokenExpiry_UnknownExpiryEndsOnlyWithParent(t *testing.T) {
	ctx, cancel := requestuser.UntilTokenExpiry(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context with no token expiry ended on its own")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	<-ctx.Done()
}

func TestUntilTokenExpiry_UnknownExpiryEndsWithParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := requestuser.UntilTokenExpiry(parent)
	defer cancel()

	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived its parent")
	}
	if errors.Is(context.Cause(ctx), requestuser.ErrTokenExpired) {
		t.Fatal("parent cancellation reported as token expiry")
	}
}

func TestUntilTokenExpiry_ExpiryPassingBetweenTicks(t *testing.T) {
	parent := requestuser.ContextWithTokenExpiry(context.Background(), time.Now().Add(60*time.Millisecond))
	ctx, cancel := requestuser.UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context ended before the token expiry")
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if !errors.Is(context.Cause(ctx), requestuser.ErrTokenExpired) {
		t.Fatalf("cause = %v, want requestuser.ErrTokenExpired", context.Cause(ctx))
	}
}

func TestErrTokenExpired_LookAlikeIsNotTheSentinel(t *testing.T) {
	if errors.Is(errors.New("auth: token expired"), requestuser.ErrTokenExpired) {
		t.Fatal("an error with the same text matched the sentinel")
	}
}

const missingUserBody = `{"type":"urn:altune:problem:auth.token_missing","title":"Authentication required","status":401,"detail":"authentication required","code":"auth.token_missing"}` + "\n"

func requireMissingUserResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate: got %q, want %q", got, "Bearer")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type: got %q, want %q", got, "application/problem+json")
	}
	if got := rec.Body.String(); got != missingUserBody {
		t.Errorf("body: got %q, want %q", got, missingUserBody)
	}
}

func TestRequireUserID(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := ids.NewUserId(uuid.New())
		ctx := requestuser.ContextWithUserID(context.Background(), uid)
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		got, ok := requestuser.RequireUserID(rec, req)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d (no error written)", rec.Code, http.StatusOK)
		}
	})

	t.Run("absent writes 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		got, ok := requestuser.RequireUserID(rec, req)
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestRequireUserID_AbsentBodyIsPinned(t *testing.T) {
	rec := httptest.NewRecorder()

	_, ok := requestuser.RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ok {
		t.Fatal("expected ok=false, got true")
	}
	requireMissingUserResponse(t, rec)
}

func TestRequireUserID_AbsentLogsTheRejection(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	req := httptest.NewRequest(http.MethodGet, "/v1/queue", nil)
	req.RemoteAddr = "203.0.113.7:5000"

	requestuser.RequireUserID(httptest.NewRecorder(), req)

	var record map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(line), &candidate); err != nil {
			t.Fatalf("log line %q is not a JSON record: %v", line, err)
		}
		if candidate["msg"] == "auth.token_rejected" {
			record = candidate
		}
	}
	if record == nil {
		t.Fatalf("no auth.token_rejected record in %q", logs.String())
	}
	want := map[string]string{
		"level":  "WARN",
		"msg":    "auth.token_rejected",
		"reason": "missing",
		"detail": "authentication required",
		"client": "203.0.113.7",
		"path":   "/v1/queue",
	}
	for key, value := range want {
		if record[key] != value {
			t.Errorf("log %s: got %v, want %q", key, record[key], value)
		}
	}
	if _, ok := record["error"]; ok {
		t.Errorf("log carries an error field: %v", record)
	}
}

func TestRequireUserID_PresentWritesNothing(t *testing.T) {
	uid := ids.NewUserId(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(requestuser.ContextWithUserID(context.Background(), uid))
	rec := httptest.NewRecorder()

	got, ok := requestuser.RequireUserID(rec, req)

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("wrote body %q headers %v, want nothing", rec.Body.String(), rec.Header())
	}
}

func TestRequireUserID_ChangesBetweenTicks(t *testing.T) {
	base := httptest.NewRequest(http.MethodGet, "/", nil)

	first := httptest.NewRecorder()
	if _, ok := requestuser.RequireUserID(first, base); ok {
		t.Fatal("first tick: expected ok=false without a user")
	}
	requireMissingUserResponse(t, first)

	uid := ids.NewUserId(uuid.New())
	second := httptest.NewRecorder()
	got, ok := requestuser.RequireUserID(second, base.WithContext(requestuser.ContextWithUserID(base.Context(), uid)))
	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("second tick: got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if second.Body.Len() != 0 {
		t.Errorf("second tick wrote body %q, want none", second.Body.String())
	}
}

func TestClientKey(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{name: "public peer", remoteAddr: "203.0.113.7:5000", want: "203.0.113.7"},
		{name: "public peer ignores spoofed forwarded header", remoteAddr: "203.0.113.7:5000", forwarded: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "private proxy peer uses last forwarded hop", remoteAddr: "172.18.0.3:5000", forwarded: []string{"10.9.9.9, 198.51.100.1"}, want: "198.51.100.1"},
		{name: "loopback proxy uses last forwarded header line", remoteAddr: "127.0.0.1:5000", forwarded: []string{"1.1.1.1", "198.51.100.2"}, want: "198.51.100.2"},
		{name: "private peer with garbage forwarded falls back to peer", remoteAddr: "172.18.0.3:5000", forwarded: []string{"not-an-ip"}, want: "172.18.0.3"},
		{name: "ipv6 keyed by /64", remoteAddr: "[2001:db8:1:2:aaaa::1]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv6 neighbours in the same /64 share a key", remoteAddr: "[2001:db8:1:2:bbbb::9]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv4-mapped ipv6 keyed as ipv4", remoteAddr: "[::ffff:203.0.113.7]:5000", want: "203.0.113.7"},
		{name: "unparseable remote addr used verbatim", remoteAddr: "pipe", want: "pipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := requestuser.ClientKey(req); got != tt.want {
				t.Errorf("ClientKey: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLimitPerUser_GivesEachUserTheirOwnBucketAndSkipsTheAnonymous(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	refusals := 0
	limit := requestuser.LimitPerUser(httpserver.RateLimit{Every: time.Hour, Burst: 1}, func() time.Time { return now },
		errors.New("refused"), func() { refusals++ })
	handler := limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(user *ids.UserId) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if user != nil {
			req = req.WithContext(requestuser.ContextWithUserID(req.Context(), *user))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	alice, bob := ids.NewUserId(uuid.New()), ids.NewUserId(uuid.New())

	first, again, other := call(&alice), call(&alice), call(&bob)
	anonymous := []int{call(nil), call(nil)}

	if first != http.StatusNoContent || other != http.StatusNoContent || again == http.StatusNoContent {
		t.Fatalf("alice=%d alice again=%d bob=%d, want 204, refused, 204", first, again, other)
	}
	if anonymous[0] != http.StatusNoContent || anonymous[1] != http.StatusNoContent || refusals != 1 {
		t.Fatalf("anonymous=%v refusals=%d, want both passed through and one refusal", anonymous, refusals)
	}
}
