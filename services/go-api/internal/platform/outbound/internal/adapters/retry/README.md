`retry` owns when an outbound request is tried again and how long it waits first, and it paces every attempt through `pacing`, so no retry transport can skip a provider's rate limit.

Only a request that is safe to send twice is retried: GET, HEAD, OPTIONS, TRACE, PUT and DELETE, or any request whose context went through `resilience.MarkRetrySafe`. Every other request is sent once, so a retry can never send an email or a webhook twice. Mark a POST only when it is a read, such as a search or lookup.
