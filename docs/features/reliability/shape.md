# Reliability (Overseer bucket)

Idea brief for shaping. Platform context: `docs/overseer.md` (vision + plugin contract), `docs/overseer-design.md` (architecture). This is bucket #2 on the menu, built on the existing plugin spine.

## Vision

At a glance, the owner should know: is the app up, what's degraded, and what's alerting. Today
there's no always-available health view — the in-process alert monitor (`internal/app/alerting.go`,
`internal/admin/alert/monitor.go`) dies with the app, exactly when you need it. The Overseer, being
a separate process, is the right home for a health view that survives the app going down.

## The idea

A Reliability bucket (a plugin implementing Collect/Store/Render) that does two things:

1. **Mirrors** go-api's health + alert state via its read surface — dependency health
   (`/health` open, `/admin/health` operator: DB/Redis/Auth), active alert conditions and recent
   history.
2. **Runs its own off-box reachability poll** — the Overseer independently confirms whether go-api
   is reachable at all. This is the role the cron `uptime-check.yml` plays today; the Overseer is
   the natural, always-on home for it.

**Rejected alternative:** mirror-only. It loses the whole point — a health view that reads *from*
the app can't tell you the app is down. The independent poll is the value-add over Mission Control.

## Assumptions

- **[load-bearing]** The Overseer's own reachability poll is the authoritative "is it up" signal;
  the mirrored data is best-effort and may be stale when the app is down.
- go-api's dependency health is readable via `/health` + `/admin/health`, and alert
  conditions/history are readable via the operator API.

## Scope / non-goals

**In:** dependency health pills (DB/Redis/Auth), active alerts + recent alert history, the
Overseer's own uptime/reachability signal, rendered as a panel.

**Out:**
- **Paging/notifying.** go-api's alert notifier already logs its own alerts; the Overseer
  *displays*, it does not page. (Non-goal for v1.)
- **Acking/muting/controlling alerts** — observe-only.
- **SLA / uptime-percentage math** — later.

## Priority

**Must:** dependency-health mirror + the Overseer's own reachability poll + active alerts.
**Then:** alert history, uptime %.

## Invariants

- **Independent down-detector:** the reachability poll does not depend on go-api being up — when
  the app is fully down, this signal still reports "down" (it is the detector).
- **Observe-only:** no path acks, mutes, or mutates alerts.
- **Bounded storage:** alert history and poll samples are bounded (ring/window).
- **Degrade-don't-crash:** when the admin health/alert read is unreachable, the bucket serves
  last-known mirrored state flagged stale, while its own poll signal stays live.
- **Owner-only:** reliability data is reachable only by the owner.

## Open questions

None blocking. Off-box poll cadence defaults to 30s (tunable) — settle during build.
