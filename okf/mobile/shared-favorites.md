---
type: Subsystem
title: Shared favorites
description: The user's saved artists, albums and tracks, and the one optimistic toggle that maintains them; the client half of the Discovery search lift.
resource: apps/mobile/src/shared/favorites/
tags: [mobile, shared, favorites, discovery, search, react-query]
---

A Favorite is a marker the user puts on an artist, album or track so that matching Discovery results rank higher. The device holds none of the meaning: it renders the heart, sends the toggle, and reads back a list. Ranking happens server-side, per request, after the shared result cache — see [scatter-gather](../backend/discovery/scatter-gather.md) for why it cannot happen anywhere else.

`useFavorites.ts` is the whole client model. One query (`discoveryKeys.favorites`, `staleTime: Infinity`, unparameterized because the list is per-user and small) plus one mutation that branches on current membership: already a favorite → `removeFavorite`, otherwise → `addFavorite`. The mutation is optimistic — `onMutate` patches the cached list, `onError` restores the snapshot it captured, `onSettled` invalidates. A heart that doesn't fill until a round-trip completes reads as broken, and the failure mode of an optimistic toggle here is cosmetic and self-correcting.

**Identity comes off the wire, never off the device.** A Favorite is keyed by `(kind, favorite_key)`, where `favorite_key` is produced by `domain.FavoriteKey` in Go — `NormalizeForMatch` on the artist name for an artist, on `artist|title` for an album or track. Every Discovery search result carries its `favorite_key`, exactly as it already carries `result_signature`, and `FavoriteButton` is handed that value rather than the strings it was derived from. The alternative — normalizing on the device to decide whether a result is favorited — means two implementations of one normalization, and they drift on precisely the inputs that matter: accented names, punctuation, doubled spaces. The visible symptom of that drift would be a heart that shows empty on a track the user has already favorited, with no error anywhere. A result that arrives without a `favorite_key` renders no heart at all.

**The fan-out is the server's.** Favoriting an artist lifts that artist's tracks and albums too. The client does not model that relationship and does not need to: `isFavorite` is an exact set membership test on the entity in front of it, and the broader lift happens in ranking. So the heart on a Don Toliver *track* stays empty when only the *artist* is favorited, even though that track will rank higher — the heart reflects what the user explicitly saved, not what benefits from it. That distinction is deliberate; conflating them would make the toggle's meaning ambiguous (unfavoriting the track would have to either do nothing or silently unfavorite the artist).

`ui/FavoriteButton.tsx` is the only surface that toggles a Favorite, and `index.ts` is the only import path — a feature reaching into `useFavorites.ts` directly would be free to build a second toggle with different optimistic behaviour. Today the single consumer is `DiscoverRow`'s trailing slot, beside the preview button.

**Untested as shipped (2026-08-02).** The slice was built with the Favorites feature and has no suite yet; the category verdicts are recorded in [shared-favorites test selection](../testing/shared-favorites.md).
