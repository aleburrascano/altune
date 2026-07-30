---
type: Mobile Feature
title: Settings
description: Account screen on the Settings tab — profile card, featured-artist backfill trigger, and sign-out.
resource: apps/mobile/src/features/settings/
tags: [mobile, feature, settings]
verified_commit: 650555c091fab169d723fa9bd938c0ab97f89541
---

The smallest feature: two files, one screen, no feature-local state machine or tests. `ui/SettingsScreen.tsx` renders a profile card (avatar = first letter of the signed-in email, read via `@shared/auth/useSession`), the account Sign Out action, and — grouped beneath it under a small "Library maintenance" label so a power-user maintenance action doesn't read as a peer of Sign Out — the featured-artist backfill button. Routing follows the standard pattern (see [app-navigation](app-navigation.md)): `app/(tabs)/settings/index.tsx` is a one-line default re-export of `SettingsScreen`, `app/(tabs)/settings/_layout.tsx` wraps a headerless `Stack` in `ScreenBoundary`, and the tab is registered third in `app/(tabs)/_layout.tsx`. Test hooks: `testID="settings-backfill-featured"` and `testID="settings-sign-out"`.

`hooks/useBackfillFeatured.ts` is a `useMutation` around `backfillFeaturedArtists` from [shared-api-client](shared-api-client.md) (`POST /v1/tracks/featured-backfill`), which runs the catalog context's `BackfillFeaturedService` — a full re-resolve of featured artists over the user's existing library, returning `{scanned, updated}` that the button renders as "Updated N of M tracks" (see [catalog/featured-artists](../backend/catalog/featured-artists.md)). **Contract**: `onSuccess` must invalidate every cache that renders featured credits — currently `libraryKeys.home`, the `libraryKeys.featuringPrefix` family, and `album-tracks` (keys from `@shared/lib/query-keys`); a new surface showing featured artists needs adding here or it will display stale credits after a backfill. **Operational caveat** (`docs/solutions/2026-07-06-synchronous-backfill-doesnt-scale.md`): the endpoint is synchronous and re-resolves every Track on each run against a ~1 req/s MusicBrainz limit, so on a real-sized library the request outlives HTTP timeouts — the mutation's success/failure UI is only trustworthy for small libraries.

Sign-out goes through `@shared/auth/useSignOut` (promoted out of the auth feature when settings and library's since-deleted ProfileSheet both consumed it; settings is now its only UI entry point): it drops the Supabase session **and** calls `queryClient.clear()`, the multi-tenancy invariant preventing user A's cached queries from leaking to user B; the root AuthGate then redirects to `/sign-in` (see [auth-feature](auth-feature.md)). The screen tracks its four-state result (`idle|pending|ok|error`) only to drive the button's pending label.

## Appearance, privacy, storage, version (2026-07-24)

The screen was profile card + sign-out + the featured-artist backfill. It now also carries the things people go to Settings looking for:

- **Appearance** — a dark/light toggle. ADR-0008 shipped v1 dark-only and `lightTheme` was drafted but never visually tuned, so the default stays dark and choosing light shows an inline caveat. The preference persists via `themePreference` (`expo-file-system`, read synchronously at store creation so the first paint is already the right scheme).
- **Privacy** — "Clear search history", which previously existed only buried in the Discover empty state. `useClearSearchHistory` optimistically empties the cache and invalidates on failure, so a failed delete restores the still-populated history rather than lying about being cleared.
- **Offline downloads** — track count and bytes, read from disk rather than summed from the pinned index (the number people check is space actually used, and the two can drift), plus remove-all behind a confirm.
- **Version** — from `expo-constants`, so it tracks `app.json` without a second place to bump. The string people read back in a bug report.

## Backfill invalidation follows the new cache families (2026-07-25)

`useBackfillFeatured`'s `onSuccess` contract is unchanged in spirit — every cache that renders featured credits must be invalidated — but the keys moved. `libraryKeys.home` no longer exists; the library's track list is a family keyed by `(query, sort)`, so the hook invalidates the `libraryKeys.tracksPrefix` prefix instead, alongside the featuring family and `album-tracks`.

The rule still stands and is still easy to break: a new surface that renders featured artists needs adding here, or it shows stale credits after a backfill.

## Grouped cards, a danger zone, and in-app issue reporting (2026-07-29)

The screen had grown by accretion: every new control arrived as another left-aligned ghost `Button` under another grey label, so `Theme: Dark` was a button whose label was its own state, and "Remove all downloads" sat one tap away from "Clear search history" with nothing but a section heading between them. The redesign keeps every existing capability and changes how they are grouped and how they read.

- **Rows are rows.** `SettingsCard` (a labelled, optionally `danger` group) and `SettingsRow` (icon glyph, label, detail line, right slot) replace the flat button column. State moved to the right-hand slot — `ThemeSegment` for Dark/Light, `318 MB` for storage, `Run`/`Running…`/`Done` for the backfill — so no label has to double as a status readout.
- **Destructive actions are quarantined.** Remove-all-downloads, clear-search-history and sign-out live together in one red-tinted `danger` card at the bottom, separated from everything else by position *and* colour. Each routes through `ConfirmDialog`; none acts on the first tap. Tests assert exactly that, because "it asks first" is the property that makes putting sign-out next to a delete safe at all.
- **Native `Alert` is gone.** Confirmations were `Alert.alert`, which cannot be themed and looks like an OS error. `ui/Dialog.tsx` is the feature's own centre-dialog shell (scrim, keyboard avoidance, scroll inside the card) and `ConfirmDialog` / `ReportIssueDialog` are its two users. Centre, not a bottom sheet — a deliberate choice, and the cost is that the keyboard pushes the dialog rather than the dialog sitting above the keyboard, which is why the shell wraps its card in a `ScrollView` inside a `KeyboardAvoidingView`.

**Report an issue** is the reason the pass happened. Altune is sideloaded onto friends' and family's devices, and their reactions were arriving as text messages. `FeedbackCard` is an accent CTA sitting directly below the profile row — rank two on the screen, above the fold, deliberately louder than any setting — and it opens `ReportIssueDialog`: three kind chips (Bug / Idea / Confusing), a four-row description field, and the diagnostics the report will carry shown *before* it is sent. `Send` stays disabled until a kind is chosen and the description reaches 10 characters, mirroring the server's floor so the tester is stopped by the UI rather than by a 400.

`reportDiagnostics(screen)` collects only what needs no new native dependency: app version from `expo-constants`, `Platform.OS` and `Platform.Version`, and the screen name passed in by the caller. Device model was considered and dropped — it would have meant `expo-device` for one line of a bug report.

Failure handling is the part worth not regressing. `submitFailureMessage` maps 400 to "describe it in a bit more detail"; everything else, including transport failure, becomes "could not reach the server — your report is saved". On failure the dialog stays open with the draft intact and the send button relabelled "Try again"; only success clears the form, and success shows the issue number (`Filed as #42`) because "sent" is weaker than a number the tester can quote back. The mutation sets `retry: false` — a report that silently retried could file duplicates, and this is one of the few writes where the user is watching the outcome.

The whole report flow lives in this feature rather than `shared/`: it has exactly one consumer, and the extraction bar is two. That also keeps the cross-feature import rule intact — settings owns the dialog it opens.

Server side: [backend/feedback](../backend/feedback.md). The endpoint is absent, not broken, on a deploy with no issue tracker configured.

## No throttle, and a way to keep going (2026-07-30)

The server's per-user hourly report cap is gone (see [backend/feedback](../backend/feedback.md) for the session that killed it), so `submitFailureMessage` no longer has a 429 branch.

The success screen gained **Send another** beside **Done**. Done closes the dialog; Send another clears the draft and stays put. The two share `clearDraft`, and `close` still calls it on the way out so a reopened dialog is never pre-filled with the last report. This exists for the same reason the throttle does not: someone with a backlog to empty should not have to walk back through Settings between each entry.
