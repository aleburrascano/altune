`logs` decides which backend log lines count as trouble.

- `New` builds the "Logs" bucket. `Start` opens go-api's log stream once and keeps the lines in a bounded ring that stays capped.
- Its snapshot surfaces how many lines were dropped, by the stream or by the ring.
