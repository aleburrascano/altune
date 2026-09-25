package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const streamWait = 5 * time.Second

// sseConn is one live client connection to an admin SSE stream over real HTTP.
type sseConn struct {
	cancel context.CancelFunc
	lines  chan string
}

func (c *sseConn) close() { c.cancel() }

// awaitData waits until the connection receives a data frame containing want.
func (c *sseConn) awaitData(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(streamWait)
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatalf("stream closed before a frame containing %q arrived", want)
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, want) {
				return
			}
		case <-deadline:
			t.Fatalf("no data frame containing %q within %s", want, streamWait)
		}
	}
}

// dialStream opens a GET against url and returns the response status. On 200
// the connection stays open and is returned; otherwise the body is returned
// and the connection is closed.
func dialStream(t *testing.T, client *http.Client, url string) (int, *sseConn, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		cancel()
		t.Fatalf("dial %s: %v", url, err)
		return 0, nil, ""
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		return resp.StatusCode, nil, string(body)
	}
	conn := &sseConn{cancel: cancel, lines: make(chan string, 256)}
	go readLines(resp.Body, conn.lines)
	return http.StatusOK, conn, ""
}

// readLines forwards each line of body to lines until the body ends, then
// closes both.
func readLines(body io.ReadCloser, lines chan<- string) {
	defer close(lines)
	defer func() { _ = body.Close() }()
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		select {
		case lines <- sc.Text():
		default:
		}
	}
}

type streamCase struct {
	name     string
	path     string
	capacity int
	// emit publishes one item carrying marker into the stream's source.
	emit func(marker string)
}

// TestAdminStreams_RejectSubscribersPastCeiling drives real SSE connections past
// each admin stream's subscriber ceiling (#996): the next connection must be
// refused with 429 and a stable code, the connections already open must keep
// receiving frames, and a disconnect must free its slot.
func TestAdminStreams_RejectSubscribersPastCeiling(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	bus := events.NewInProcessBus()
	tap := eventtap.New(bus)
	feed := eventtap.NewFeed()
	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer func() {
		stopFeed()
		feed.Shutdown(context.Background())
	}()
	feed.Start(feedCtx, tap)

	r := chi.NewRouter()
	handler.New(nil, ring).WithEventFeed(feed).RegisterData(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	client := srv.Client()

	user := shared.NewUserId(uuid.New())
	cases := []streamCase{
		{
			name:     "logs",
			path:     "/logs/stream",
			capacity: logging.MaxSubscribers,
			emit:     func(marker string) { slog.Info(marker) },
		},
		{
			name:     "events",
			path:     "/events/stream",
			capacity: eventtap.MaxSubscribers,
			emit:     func(marker string) { tap.Publish(context.Background(), user, marker, nil) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := srv.URL + tc.path
			open := make([]*sseConn, 0, tc.capacity)
			defer func() {
				for _, c := range open {
					c.close()
				}
			}()
			for i := 0; i < tc.capacity; i++ {
				status, conn, body := dialStream(t, client, url)
				if status != http.StatusOK {
					t.Fatalf("subscriber %d of %d: status = %d, want 200 (body %q)", i+1, tc.capacity, status, body)
				}
				open = append(open, conn)
			}

			status, conn, body := dialStream(t, client, url)
			if conn != nil {
				conn.close()
			}
			if status != http.StatusTooManyRequests {
				t.Fatalf("subscriber past ceiling: status = %d, want 429", status)
			}
			var errBody struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal([]byte(body), &errBody); err != nil || errBody.Code != "admin.stream_subscriber_limit" {
				t.Errorf("rejection body = %q, want code admin.stream_subscriber_limit", body)
			}

			marker := "marker-" + tc.name + "-" + uuid.NewString()
			tc.emit(marker)
			for _, c := range open {
				c.awaitData(t, marker)
			}

			open[0].close()
			open = open[1:]
			deadline := time.Now().Add(streamWait)
			for {
				status, conn, _ := dialStream(t, client, url)
				if status == http.StatusOK {
					open = append(open, conn)
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("slot not released after a disconnect: status = %d", status)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// TestAdminStreams_OutliveRouteWriteDeadline guards #1018: the admin live tails
// run behind the router-wide write deadline, and must keep delivering frames
// long after it has elapsed.
func TestAdminStreams_OutliveRouteWriteDeadline(t *testing.T) {
	const routeDeadline = 150 * time.Millisecond

	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	bus := events.NewInProcessBus()
	tap := eventtap.New(bus)
	feed := eventtap.NewFeed()
	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer func() {
		stopFeed()
		feed.Shutdown(context.Background())
	}()
	feed.Start(feedCtx, tap)

	r := chi.NewRouter()
	r.Use(httputil.WriteDeadline(routeDeadline))
	r.Use(httputil.RequestLogger)
	handler.New(nil, ring).WithEventFeed(feed).RegisterData(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	user := shared.NewUserId(uuid.New())
	cases := []streamCase{
		{name: "logs", path: "/logs/stream", emit: func(marker string) { slog.Error(marker) }},
		{name: "events", path: "/events/stream", emit: func(marker string) { tap.Publish(context.Background(), user, marker, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, conn, body := dialStream(t, srv.Client(), srv.URL+tc.path)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", status, body)
			}
			defer conn.close()

			time.Sleep(3 * routeDeadline)
			marker := "late-" + tc.name + "-" + uuid.NewString()
			tc.emit(marker)
			conn.awaitData(t, marker)
		})
	}
}
