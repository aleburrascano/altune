package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConsumerLastErrorClearsOnRecovery(t *testing.T) {
	stub := &stubSSE{holdOpen: true, events: []goapi.Event{{Type: "alive", Timestamp: time.Now().UTC()}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	recv(t, c.Events())
	eventually(t, "status up before the drop", func() bool { return c.Status() == goapi.StatusUp })

	stub.setDown(true)
	srv.CloseClientConnections()
	eventually(t, "status connecting right after the drop", func() bool { return c.Status() == goapi.StatusConnecting })
	eventuallyWithin(t, "status down once reconnects fail past the 10s grace", 15*time.Second, func() bool {
		return c.Status() == goapi.StatusDown
	})
	if c.LastError() == nil {
		t.Fatal("LastError = nil while down, want the drop's error")
	}

	stub.setDown(false)
	eventually(t, "status up after recovery", func() bool { return c.Status() == goapi.StatusUp })
	recv(t, c.Events())
	if err := c.LastError(); err != nil {
		t.Fatalf("LastError after recovery = %v, want nil", err)
	}
}
