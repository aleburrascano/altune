`retry` owns when an outbound request is tried again and how long it waits first, and it paces every attempt through `pacing`, so no retry transport can skip a provider's rate limit.
