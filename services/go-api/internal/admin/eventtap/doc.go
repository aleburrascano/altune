// Package eventtap mirrors the service's domain events to the admin console
// without changing what publishing them does. It holds the Tap, an
// events.Publisher decorator that copies each published event onto one
// system-wide channel; the Feed that drains that channel into a rateWindow
// (per-type counts over a trailing window) and a broadcaster (the bounded set
// of live console subscribers); and TapEvent, the projection both serve.
package eventtap
