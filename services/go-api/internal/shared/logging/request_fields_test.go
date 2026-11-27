package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"strings"
	"testing"
)

func TestRequestFields_StampEveryLineOfTheContext(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	fields := logging.RequestFields{UserID: "user-1", Route: "/v1/tracks/{id}"}
	ctx := logging.ContextWithRequestFields(context.Background(), func() logging.RequestFields { return fields })

	logger.InfoContext(ctx, "first")
	fields.UserID = "user-2"
	logger.InfoContext(ctx, "second")

	lines := strings.Split(strings.TrimSpace(out()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"user_id":"user-1"`) || !strings.Contains(lines[0], `"route":"/v1/tracks/{id}"`) {
		t.Errorf("first line lacks the bound user and route: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"user_id":"user-2"`) {
		t.Errorf("second line lacks the user resolved at log time: %s", lines[1])
	}
}

func TestRequestFields_KeepAnExplicitKeyAndSkipEmptyFields(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.ContextWithRequestFields(context.Background(), func() logging.RequestFields {
		return logging.RequestFields{UserID: "bound"}
	})

	logger.InfoContext(ctx, "explicit", logging.KeyUserID, "explicit")

	line := out()
	if got := strings.Count(line, `"user_id"`); got != 1 || !strings.Contains(line, `"user_id":"explicit"`) {
		t.Errorf("line has %d user_id keys, want one holding explicit: %s", got, line)
	}
	if strings.Contains(line, `"route"`) {
		t.Errorf("line carries a route although none was resolved: %s", line)
	}
}
