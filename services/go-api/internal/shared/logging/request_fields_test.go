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

func TestContextWithSubjectUser_StampsBackgroundWorkWithTheUserItIsFor(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.ContextWithSubjectUser(context.Background(), "subject-1")

	logger.InfoContext(ctx, "background line")

	if line := out(); !strings.Contains(line, `"user_id":"subject-1"`) {
		t.Errorf("line lacks the subject user: %s", line)
	}
}

func TestContextWithSubjectUser_KeepsTheRouteAndReplacesTheRequestUser(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	request := logging.ContextWithRequestFields(context.Background(), func() logging.RequestFields {
		return logging.RequestFields{UserID: "caller", Route: "/v1/tracks/{id}/retry"}
	})
	ctx := logging.ContextWithSubjectUser(request, "owner")

	logger.InfoContext(ctx, "work for the owner")

	line := out()
	if !strings.Contains(line, `"user_id":"owner"`) || strings.Contains(line, `"caller"`) {
		t.Errorf("line should carry the subject user and not the request user: %s", line)
	}
	if !strings.Contains(line, `"route":"/v1/tracks/{id}/retry"`) {
		t.Errorf("line lost the request route: %s", line)
	}
}

func TestContextWithSubjectUser_AnEmptyUserKeepsTheRequestUser(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	request := logging.ContextWithRequestFields(context.Background(), func() logging.RequestFields {
		return logging.RequestFields{UserID: "caller"}
	})
	ctx := logging.ContextWithSubjectUser(request, "")

	logger.InfoContext(ctx, "no subject")

	if line := out(); !strings.Contains(line, `"user_id":"caller"`) {
		t.Errorf("line lost the request user: %s", line)
	}
}

func TestContextWithSubjectUser_AnExplicitKeyStillWins(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.ContextWithSubjectUser(context.Background(), "subject")

	logger.InfoContext(ctx, "explicit", logging.KeyUserID, "explicit")

	line := out()
	if got := strings.Count(line, `"user_id"`); got != 1 || !strings.Contains(line, `"user_id":"explicit"`) {
		t.Errorf("want one explicit user_id, got: %s", line)
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
