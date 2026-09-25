package handler

import (
	"context"
	"log/slog"
	"time"
)

// The kill-switch routes flip an in-memory runloop gate (alert monitor, eval
// meter, acquisition scheduler, background jobs) so an operator can silence a
// misbehaving loop without a redeploy. Each gate is per process and resets on
// restart; admin auth is a bearer token (no cookies), so these POSTs need no
// CSRF token, matching the other state-changing admin routes.

// auditKillSwitch writes the one admin.kill_switch record every flip leaves, so
// the event name and the loop/paused/actor/at fields an operator greps for are
// spelled once (#1990). Naming the actor and the time here rather than at each
// call site is what closes #1998: a flip cannot reach the log without saying
// who paused the loop and when. Only a field one loop alone can supply — the
// job name — arrives as extra.
func auditKillSwitch(ctx context.Context, loop string, paused bool, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("loop", loop),
		slog.Bool("paused", paused),
		slog.String("actor", operatorActor(ctx)),
		slog.Time("at", time.Now().UTC()),
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "admin.kill_switch", append(attrs, extra...)...)
}
