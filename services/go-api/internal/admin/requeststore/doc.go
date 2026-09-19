// Package requeststore captures the provider traffic behind one correlated
// request, so an operator can read back what the discovery pipeline actually
// sent and received. It holds the bounded in-memory Store of RequestRecord
// (the Exchange round trips plus the search and detail traces projected onto
// them), the http.RoundTripper wrappers that feed it (correlatedTransport for
// live traffic, RerunRecorder for a replay), the redaction and size estimation
// that bound and sanitize what is kept, and the rerun result types the admin
// inspector returns.
package requeststore
