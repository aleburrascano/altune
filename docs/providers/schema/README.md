# Provider field ledgers

One YAML file per provider lists **every field that provider sends us**, and says for each one
whether our adapter reads it. We don't own the music, so no field gets dropped without a written
reason. A test fails the build the moment one does.

The prose pages one folder up describe what each provider *can* do. These ledgers are the
machine-checked record of what it *does* send, and what we do with it.

## Reading a ledger

```yaml
provider: deezer
reference: "none: ..."            # the machine-readable spec it was checked against, or why there is none
entities:
  track:                          # one response the adapter decodes
    fixtures:                     # the fixed IDs every refresh fetches
      - {id: "3135556", why: a normal single}
    fields:
      - path: contributors[].name # where the field sits; [] is an array element, * an ID-keyed map
        type: string              # JSON type seen; a|b when it varies
        seen: [live]              # where it was seen: the reference, a live fixture, or both
        read: true                # the adapter's Go code decodes it
      - path: isrc
        type: string
        seen: [live]
        read: false
        unused: "GAP: ..."        # why we don't read it; GAP marks data we would want
      - path: upc
        type: unseen
        seen: []
        read: true
        absent: "BUG: ..."        # the adapter decodes it but this response never sends it
```

- `GAP:` is a field the provider sends that would help us. Each one is a possible improvement.
- `BUG:` is a field our code relies on that this response never sends. Each one is a real bug.
- `by_key: true` marks a read found only by key name, for adapters that walk raw JSON maps
  (YouTube Music). Treat it as a likely read, not a proven one.

## The check

`go test ./cmd/providerschema` (from `services/go-api`, part of CI) fails when a field:

- is not read and has no `unused` reason,
- is decoded but never sent and has no `absent` reason,
- is no longer sent anywhere,
- or has a `read` flag that no longer matches the adapter's code.

After changing an adapter's decode structs, re-mark the read flags offline:

```sh
go test ./cmd/providerschema -update-schema
```

Then write a reason for every field it reports.

## Refreshing from the providers

```sh
cd services/go-api
go run ./cmd/providerschema -provider deezer -env .env.development
```

It fetches the reference (Apple Music, Google Sheets) and every fixture, merges the fields, re-marks
the read flags and rewrites the ledger. On any error it writes nothing. A refresh of an unchanged
provider gives a byte-identical file. A diff means the provider changed what it sends.

| Needs | Providers |
|---|---|
| nothing | applemusic, deezer, itunes, musicbrainz, spotify, ytmusic |
| a key from `-env` | discogs (`DISCOGS_TOKEN`), genius (`GENIUS_ACCESS_TOKEN`), fanarttv (`FANARTTV_API_KEY`), trackers (`GOOGLE_SHEETS_API_KEY`) |
| `yt-dlp` on `PATH` | soundcloud |

TheAudioDB uses `THEAUDIODB_API_KEY` when it is set, and its public test key `123` otherwise.

Known moving parts:

- SoundCloud's `track_related` rotates its results, so a rare field can come and go between refreshes.
- Search entities follow live rankings. Fixtures are exact phrases to keep them stable.
- `trackers` has no live fixtures yet, so only its reference side is checked.

## Adding a provider

1. Write `cmd/providerschema/<provider>.go` in the shape the others use: endpoint table, provider entry, source, request helpers.
2. Add it to `providers` in `cmd/providerschema/providers.go`.
3. Seed `docs/providers/schema/<provider>.yaml` with `provider`, `reference` and each entity's fixtures, with `fields: []`.
4. Run the refresh, then give every unread field a reason.
