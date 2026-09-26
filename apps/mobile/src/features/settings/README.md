# settings — seam map

The account/preferences screen. One route entry: `src/app/(tabs)/settings/index.tsx`, which
renders `ui/SettingsScreen.tsx`. Everything else in this folder is a card, a hook, or a pure
module `SettingsScreen` wires together.

## Cards (`ui/`)

`SettingsScreen.tsx` lays out one `SettingsCard` per section, in order: appearance
(`AppearanceCard.tsx`), library (`LibraryCard.tsx`), offline downloads (`OfflineDownloadsCard.tsx`,
gated by `offlineDownloadsSupported`, see below), danger zone (`DangerZoneCard.tsx`), and
feedback (`FeedbackCard.tsx`, opens `ReportIssueModal.tsx`).

- **Danger zone rows come from one action list.** `DangerZoneCard.tsx` never builds a row
  itself; it renders whatever `buildDangerZoneActions` (in `dangerZoneActions.ts`) returns —
  one `{ row, confirm }` pair per destructive action (remove downloads, clear search history,
  sign out). Adding or reordering a danger-zone action means editing that list, not the card.
- **`failureCopyForAction.ts` and `failureCopyForReport.ts` are the only error-to-copy mapping.**
  Every failure shown in this feature (a danger-zone action's failed status, a report submit
  error) goes through one of these two; no card formats an error message inline.

## The offline-downloads seam

This is the seam the web port needs to find:

- `downloadStatsModel.ts` — pure. Turns the pinned-file entries and byte total from
  `@shared/offline/pinnedStore` into a `DownloadStats` (count, size, usage bucket, label). No
  native or React import.
- `hooks/useDownloadStats.ts` — reads `usePinnedStore` and `pinnedByteTotal` from
  `@shared/offline/pinnedStore`, feeds them through `downloadStatsModel.downloadStats`.
- `hooks/useRemoveDownloads.ts` — wraps a `DownloadStats` together with the store's `unpinAll`
  action and its last outcome (`lastUnpinAll`) into one `RemoveDownloads` value, so callers carry
  one prop instead of three.
- `dangerZoneActions.ts` — the only consumer of `RemoveDownloads` in the danger zone: builds the
  "Remove all downloads" row and confirm from it, including the partial-removal failure copy.
- `OfflineDownloadsCard.tsx` — the read-only usage display; takes the same `DownloadStats`.

`SettingsScreen.tsx` is the only place these compose: it calls `useDownloadStats()` then
`useRemoveDownloads(stats)` and passes the result down to both `OfflineDownloadsCard` and
`DangerZoneCard`. A web port that has no on-device pinned store swaps out
`@shared/offline/pinnedStore` (or these two hooks) at that seam; `downloadStatsModel.ts` and
`dangerZoneActions.ts` stay as-is since neither imports anything native.

**Where web omits downloads today:** `@shared/offline/offlineSupport.ts` exports
`offlineDownloadsSupported = Platform.OS !== 'web'`. `SettingsScreen.tsx` uses it to skip
rendering `OfflineDownloadsCard` on web. The danger zone's "Remove all downloads" row is not
gated the same way — it is driven purely by `DownloadStats.usage`, so a web build still needs a
downloads seam that reports `usage: 'none'` (or a real web-storage answer) rather than skipping
the hook call.

## The report seam

`FeedbackCard.tsx` opens `ReportIssueModal.tsx`, which renders `ReportFormView.tsx` (the form) or
`ReportSentView.tsx` (the success state).

- `reportRules.ts` — pure. `MIN_MESSAGE_LENGTH`/`MAX_MESSAGE_LENGTH` and `isReportReady`, the
  only place that decides whether the submit button is enabled.
- `reportDiagnostics.ts` — pure except for reading `expo-constants` and `react-native`'s
  `Platform`; builds the `app_version`/`platform`/`os_version`/`screen` bundle attached to every
  report.
- `hooks/useSubmitReport.ts` — the `@tanstack/react-query` mutation that calls
  `submitReport` from `@shared/api-client/feedback`, keyed by a per-draft idempotency key so a
  retry cannot file a duplicate issue from the same draft.

## Everything else

- `hooks/useAccountEmail.ts`, `hooks/useClearSearchHistory.ts`, `hooks/backfillStatus.ts`,
  `hooks/useBackfillFeatured.ts` back the account row, danger-zone history action, and the
  library backfill indicator respectively; none of them is a pure module, despite the folder
  name (`backfillStatus.ts` is tracked for a future move in a separate ticket).
