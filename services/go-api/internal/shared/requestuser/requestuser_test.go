package requestuser_test

import (
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := requestuser.UntilTokenExpiry(context.Background())
		defer cancel()

		time.Sleep(time.Hour)
		synctest.Wait()
		select {
		case <-ctx.Done():
			t.Fatal("context with no token expiry ended on its own")
		default:
		}

		cancel()
		<-ctx.Done()
	})
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
	synctest.Test(t, func(t *testing.T) {
		parent := requestuser.ContextWithTokenExpiry(context.Background(), time.Now().Add(60*time.Millisecond))
		ctx, cancel := requestuser.UntilTokenExpiry(parent)
		defer cancel()

		time.Sleep(20 * time.Millisecond)
		synctest.Wait()
		select {
		case <-ctx.Done():
			t.Fatal("context ended before the token expiry")
		default:
		}

		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-ctx.Done():
		default:
			t.Fatal("context outlived the token expiry")
		}
		if !errors.Is(context.Cause(ctx), requestuser.ErrTokenExpired) {
			t.Fatalf("cause = %v, want requestuser.ErrTokenExpired", context.Cause(ctx))
		}
	})
}

func TestErrTokenExpired_LookAlikeIsNotTheSentinel(t *testing.T) {
	if errors.Is(errors.New("auth: token expired"), requestuser.ErrTokenExpired) {
		t.Fatal("an error with the same text matched the sentinel")
	}
}

func TestUserID(t *testing.T) {
	t.Run("returns the signed-in user", func(t *testing.T) {
		uid := ids.NewUserId(uuid.New())
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(requestuser.ContextWithUserID(context.Background(), uid))

		if got := requestuser.UserID(req); got.UUID() != uid.UUID() {
			t.Errorf("UserID = %v, want %v", got.UUID(), uid.UUID())
		}
	})

	t.Run("a route without the auth middleware fails closed", func(t *testing.T) {
		defer func() {
			if got := recover(); !strings.Contains(fmt.Sprint(got), "no signed-in user") {
				t.Errorf("recovered %v, want the no-signed-in-user panic", got)
			}
		}()

		requestuser.UserID(httptest.NewRequest(http.MethodGet, "/", nil))

		t.Error("UserID returned for a request with no user")
	})
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
