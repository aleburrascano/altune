`logs` decides which backend log lines count as trouble.

- `New` builds the "Logs" bucket. `Start` opens go-api's log stream once and keeps the lines in a bounded ring that stays capped.
- Lines below the minimum level (`OVERSEER_LOGS_MIN_LEVEL`) are filtered out, the rest grade the bucket, and sensitive fields are redacted before a line is stored.
- Its snapshot surfaces how many lines were dropped, by the stream or by the ring.
