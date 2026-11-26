`pacing` owns how fast the backend may call each provider host.

A request that would wait longer than three intervals of its host's rate, or past its own deadline, is refused at once with an error that wraps `shared/queuetimeout.Err` and `context.DeadlineExceeded`, so a burst of callers cannot pile up behind a slow host. A refused request keeps no slot. Where an adapter once paced a host as well, the strictest of the two rates is the one kept here.
