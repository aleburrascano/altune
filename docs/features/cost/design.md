# Cost — design

Brief: `docs/cost.md`. The first bucket that reads a source **other than go-api**.

## Anchor & inherited invariants
- **Anchor:** provider API usage + OCI infra spend, one glance.
- **Inherited:** bounded · owner-only · observe-only-on-infra · degrade-per-source · read-only least-priv creds.

## Grounded in
- Provider usage: the **cost-enabler** `/admin/metrics/live` `providers` field (its epic). Overseer read client `internal/goapi/`.
- OCI spend: **no OCI SDK in either go.mod** — a new dependency. The Overseer runs on an OCI box.

## Significance
**Significant** — a new external dependency (OCI Go SDK), a new external data source, and cloud
credentials. Walk it.

## Design decisions
- **Boundaries:** new bucket `internal/buckets/cost/`. Two independent sources behind two seams:
  1. **Provider usage** — a new additive read `internal/goapi/cost_reads.go` (operator `GET
     /admin/metrics/live` `providers`), allowlist-registered.
  2. **OCI spend** — a new `internal/oci/` client in the Overseer using the **OCI Go SDK**, authed by
     **instance principal** (confirmed) — no stored key; read-only **usage-api** only.
- **Auth / security (the crux):** instance principal + an IAM policy that grants the Overseer's
  instance **only** `usage-api` read. The client never holds a long-lived key and never calls a
  mutating OCI API. *Over:* a stored API key — rejected, a secret to leak; *over:* shelling the `oci`
  CLI — rejected, a Go SDK call is testable and dependency-scoped.
- **Data & state:** bounded rollups of spend + counts (`core.RingStore`).
- **Failure/degradation:** **independent per source** (like domain-quality) — OCI unreachable flags
  only the spend half stale; the enabler down flags only the usage half.
- **Infra fit:** adds `github.com/oracle/oci-go-sdk` to `services/overseer/go.mod`.

## Architectural invariants (spine)
- OCI access is **instance-principal, read-only usage-api**; the bucket never mutates infra; no OCI
  secret reaches logs/render.
- Provider-usage read is additive (own `internal/goapi` file; never edits `client.go`).
- The two sources degrade independently.

## Slice — two leaves (the OCI half is the risky, isolated one)
1. **Provider-usage half** — `cost_reads.go` + the usage panel. **Blocked on the cost-enabler epic.**
2. **OCI-spend half** — the `internal/oci` instance-principal read-only client + the spend panel +
   the go.mod dep. Independent of the enabler; the heavier, security-sensitive leaf.
Both register into the one `cost` bucket; sequence the second after the first on the bucket files.

## Depends on
The cost-enabler epic (provider-usage half only).
