---
type: Mobile Feature
title: Settings
description: Account screen on the Settings tab — profile card, featured-artist backfill trigger, and sign-out.
resource: apps/mobile/src/features/settings/
tags: [mobile, feature, settings]
verified_commit: 088f954bbb7ca36e7bb99d929f388f356df3019a
---

The smallest feature: two files, one screen, no feature-local state machine or tests. `ui/SettingsScreen.tsx` renders a profile card (avatar = first letter of the signed-in email, read via `@shared/auth/useSession`), the account Sign Out action, and — grouped beneath it under a small "Library maintenance" label so a power-user maintenance action doesn't read as a peer of Sign Out — the featured-artist backfill button. Routing follows the standard pattern (see [app-navigation](app-navigation.md)): `app/(tabs)/settings/index.tsx` is a one-line default re-export of `SettingsScreen`, `app/(tabs)/settings/_layout.tsx` wraps a headerless `Stack` in `ScreenBoundary`, and the tab is registered third in `app/(tabs)/_layout.tsx`. Test hooks: `testID="settings-backfill-featured"` and `testID="settings-sign-out"`.

`hooks/useBackfillFeatured.ts` is a `useMutation` around `backfillFeaturedArtists` from [shared-api-client](shared-api-client.md) (`POST /v1/tracks/featured-backfill`), which runs the catalog context's `BackfillFeaturedService` — a full re-resolve of featured artists over the user's existing library, returning `{scanned, updated}` that the button renders as "Updated N of M tracks" (see [catalog](../backend/catalog.md)). **Contract**: `onSuccess` must invalidate every cache that renders featured credits — currently `libraryKeys.home`, the `libraryKeys.featuringPrefix` family, and `album-tracks` (keys from `@shared/lib/query-keys`); a new surface showing featured artists needs adding here or it will display stale credits after a backfill. **Operational caveat** (`docs/solutions/2026-07-06-synchronous-backfill-doesnt-scale.md`): the endpoint is synchronous and re-resolves every Track on each run against a ~1 req/s MusicBrainz limit, so on a real-sized library the request outlives HTTP timeouts — the mutation's success/failure UI is only trustworthy for small libraries.

Sign-out goes through `@shared/auth/useSignOut` (promoted out of the auth feature when settings and library's since-deleted ProfileSheet both consumed it; settings is now its only UI entry point): it drops the Supabase session **and** calls `queryClient.clear()`, the multi-tenancy invariant preventing user A's cached queries from leaking to user B; the root AuthGate then redirects to `/sign-in` (see [auth-feature](auth-feature.md)). The screen tracks its four-state result (`idle|pending|ok|error`) only to drive the button's pending label.
