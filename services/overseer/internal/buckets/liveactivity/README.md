`liveactivity` decides what users are doing right now.

- `New` builds the "Live activity" bucket. `Start` opens go-api's event stream once, and its feed keeps the latest events in a bounded ring.
- Its snapshot counts the events the stream dropped together with the ones the ring evicted, and says it is connecting while the source reconnects.
- A down source degrades the bucket to source-down.
