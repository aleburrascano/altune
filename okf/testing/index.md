---
type: Index
title: Test selections
description: Per-slice records of which taxonomy categories apply, which were rejected and why, and the mutation result.
tags: [index, testing]
---

One record per slice, written by `/qa-slice` step 2 and updated at step 7. The categories come from [test-taxonomy](../playbooks/test-taxonomy.md).

A category listed as selected without its done-condition met is a lie; a category missing from a record entirely is a hole. Rejections carry reasons.

- [shared-events](shared-events.md) — mobile SSE event bus and cache patchers
- [shared-acquisition](shared-acquisition.md) — download lifecycle and track-status overlay (**not started**; pre-filled with harvested regression candidates)
