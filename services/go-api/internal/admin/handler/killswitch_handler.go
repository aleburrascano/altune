package handler

import (
	"context"
	"log/slog"
)

// The kill-switch routes flip an in-memory runloop gate (alert monitor, eval
// meter, acquisition scheduler, background jobs) so an operator can silence a
// misbehaving loop without a redeploy. Each gate is per process and resets on
// restart; admin auth is a bearer token (no cookies), so these POSTs need no
// CSRF token, matching the other state-changing admin routes.

// auditKillSwitch writes the one admin.kill_switch record every flip leaves, so
// the event name and the loop/paused fields an operator greps for are spelled
// once (#1990). Fields only one loop can supply — the job name, the actor —
// arrive as extra, so no loop is forced to widen what it logs.
func auditKillSwitch(ctx context.Context, loop string, paused bool, extra ...slog.Attr) {
	attrs := append([]slog.Attr{slog.String("loop", loop), slog.Bool("paused", paused)}, extra...)
	slog.LogAttrs(ctx, slog.LevelInfo, "admin.kill_switch", attrs...)
}
