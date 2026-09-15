# Cost (Overseer bucket)

Idea brief. Bucket #8. Built on the plugin spine. **First bucket that reads a source other than go-api.**

## Vision
What am I spending — provider API usage **and** the OCI infra bill — at a glance.

## The idea
A Cost bucket that renders two signals:
1. **Provider API usage** — read the per-provider call counts from the **cost-enabler** endpoint on go-api.
2. **OCI infra spend** — pull the current/period spend from **OCI's usage/billing API** (a new external source; the Overseer runs on OCI).

Both bounded, degrading independently (one source down flags only its half — like the domain-quality bucket).

**Rejected:** one source only (owner wants both); scraping the OCI console UI.

## Assumptions
- **[load-bearing]** OCI spend is pullable programmatically with credentials the Overseer can hold safely.

## Scope / non-goals
**In:** provider-usage panel (from the enabler) + OCI infra-spend panel (from the OCI usage API), bounded history.
**Out:** cost forecasting/alerting; per-request cost attribution; any action on infra (read-only).

## Priority
**Must:** provider-usage panel + OCI-spend panel. **Then:** trends.

## Invariants
- Bounded storage · owner-only · degrade-to-stale **per source** (independent).
- **Observe-only on infra:** the bucket reads OCI billing and **never modifies infrastructure**.
- OCI credentials are **read-only, least-privilege**; no secret leaks into logs/render.

## Open questions (need a decision)
- **OCI auth path:** the Overseer runs on an OCI box — use an **instance principal** (no stored key) or a **read-only API key**? And call the **OCI Go SDK usage-api** directly, or shell the `oci` CLI? (My recommendation: instance principal + OCI Go SDK, read-only usage-api.)

## Depends on
The cost-enabler epic (for the provider-usage half).
