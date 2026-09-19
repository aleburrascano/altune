// Package alert pages an operator when a named condition starts failing. It
// holds the Monitor, whose loop evaluates each Condition on a ticker behind a
// runtime kill switch and a leadership scope; the Alert a firing condition
// produces and the Severity that decides whether it is logged or pushed; and
// the AlertNotifier implementations behind a push (NtfyNotifier, NopNotifier).
// The conditions themselves live with the code they watch, in
// internal/app/alerting.go.
package alert
