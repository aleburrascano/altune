`heartbeat` decides when the overseer itself counts as alive.

- `New` builds the "Heartbeat" bucket. Each collect tick after the first records the gap since the previous tick, and that tick gap is its key series.
- It reads no other service, so it keeps working when go-api is down.
