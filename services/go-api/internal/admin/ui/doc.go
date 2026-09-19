// Package ui is the admin console's front end: one index.html page, embedded
// into the binary as IndexHTML so the service ships its own operator UI with
// no static-file deployment beside it. It holds no logic. The page's own
// policy — its CSP, framing and cache headers — is set by the handler that
// serves IndexHTML, not here.
package ui
