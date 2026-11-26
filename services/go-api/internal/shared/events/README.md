`events` owns the event bus, the event types it carries, the typed payload each type publishes, and the id floor that keeps event ids rising across restarts.

A publisher hands the bus one `Payload`; the payload names its own event type, so a type and its fields cannot drift apart. The JSON of each payload is the wire contract that deployed clients parse, and `payloads_test.go` pins it.
