`outbound` owns which outbound addresses the backend may dial.

`NewLiveTransport` retries only requests that are safe to send twice: GET, HEAD, OPTIONS, TRACE, PUT and DELETE. A POST is sent once unless its context went through `shared/retrysafe.Mark`, which a caller applies only to a POST that reads, such as a search or lookup.
