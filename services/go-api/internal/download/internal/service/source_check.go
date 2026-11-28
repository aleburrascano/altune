package service

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type Check struct {
	Name string
	Run  func(context.Context) error
}

func RunChecks(ctx context.Context, now clock.Func, checks []Check) error {
	if now == nil {
		panic("service.RunChecks: a nil clock")
	}
	var dark []string
	for _, check := range checks {
		started := now()
		err := check.Run(ctx)
		logCheckResult(ctx, check.Name, now().Sub(started), err)
		if err != nil {
			dark = append(dark, fmt.Sprintf("%s (%q)", check.Name, err.Error()))
		}
	}
	if len(dark) == 0 {
		return nil
	}
	return fmt.Errorf("sources dark: %s", strings.Join(dark, ", "))
}

func logCheckResult(ctx context.Context, source string, duration time.Duration, err error) {
	if err != nil {
		slog.InfoContext(ctx, "acquisition.source_canary",
			"source", source, "ok", false, logging.KeyDurationMS, duration.Milliseconds(), logging.KeyError, err)
		return
	}
	slog.InfoContext(ctx, "acquisition.source_canary",
		"source", source, "ok", true, logging.KeyDurationMS, duration.Milliseconds())
}
