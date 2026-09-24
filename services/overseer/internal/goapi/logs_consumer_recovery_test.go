package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogsConsumerLastErrorClearsOnRecovery(t *testing.T) {
	stub := &stubLogSSE{holdOpen: true, records: []goapi.LogRecord{{Time: time.Now().UTC(), Level: "INFO", Message: "alive"}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	recvLog(t, c.Records())
	eventually(t, "status up before the drop", func() bool { return c.Status() == goapi.StatusUp })

	stub.setDown(true)
	srv.CloseClientConnections()
	eventually(t, "status down after the drop", func() bool { return c.Status() == goapi.StatusDown })
	if c.LastError() == nil {
		t.Fatal("LastError = nil while down, want the drop's error")
	}

	stub.setDown(false)
	eventually(t, "status up after recovery", func() bool { return c.Status() == goapi.StatusUp })
	recvLog(t, c.Records())
	if err := c.LastError(); err != nil {
		t.Fatalf("LastError after recovery = %v, want nil", err)
	}
}
