// Package evalmeter runs the search-quality smoke eval on a schedule and keeps
// its latest verdict for the admin console. It holds the Meter, whose loop
// invokes a Runner on a ticker behind a runtime kill switch and a leadership
// scope; the Result one run produces; and the Status and State vocabulary the
// operator surface reads. The eval itself lives outside the module, in
// internal/app/eval_runner.go, and reaches the Meter as a Runner.
package evalmeter
