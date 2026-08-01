---
type: Index
title: Test selections
description: Per-slice records of which taxonomy categories apply, which were rejected and why, and the mutation result.
tags: [index, testing]
---

Rationale, the slice ledger, and the outstanding cross-slice findings: [programme](programme.md).

One record per slice, written by `/qa-slice` step 2 and updated at step 7. The categories come from [test-taxonomy](../playbooks/test-taxonomy.md).

A category listed as selected without its done-condition met is a lie; a category missing from a record entirely is a hole. Rejections carry reasons.

- [programme](programme.md) — why the suite was rebuilt, the six decisions, the slice ledger, and the outstanding findings
- [shared-events](shared-events.md) — mobile SSE event bus and cache patchers
- [shared-acquisition](shared-acquisition.md) — download lifecycle and track-status overlay
- [shared-playback](shared-playback.md) — the client-owned Queue state machine and its facade
- [shared-offline](shared-offline.md) — pinned downloads: the on-disk audio, its index, and the launch reconciliation
- [shared-api-client](shared-api-client.md) — the typed HTTP client: auth injection, deadlines, and the error taxonomy
- [shared-lib](shared-lib.md) — the shared utilities: the query-key topology, the extras parsers, and the discover↔detail handoff
- [shared-telemetry](shared-telemetry.md) — the rotating session id, the durable critical outbox, and the event POST
- [shared-auth](shared-auth.md) — the Supabase client singleton, the keychain adapter, and the identity boundary between two users on one device
- [shared-playlists](shared-playlists.md) — the playlist write module: batch membership, optimistic rollback, and the save-on-pick seam
