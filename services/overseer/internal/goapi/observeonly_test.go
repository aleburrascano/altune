package goapi_test

import (
	"altune/overseer/internal/goapi"
	"reflect"
	"strings"
	"testing"
)

// readOnlyMethods is the exhaustive allowlist of exported methods the client may
// expose. Every entry is a GET. Adding any method forces a deliberate edit here,
// and adding a non-read one trips the forbidden-verb guard below — so the
// observe-only invariant cannot regress silently.
var readOnlyMethods = map[string]bool{
	"Health":             true,
	"AdminHealth":        true,
	"AdminEval":          true,
	"AdminAcquisition":   true,
	"AdminMetricsLive":   true,
	"AdminProviderUsage": true,
}

// mutatingVerbs are name fragments that betray a write/command/mutating method.
// The Overseer authenticates as an operator with no write scope; the client must
// have no such method at all.
var mutatingVerbs = []string{
	"post", "put", "patch", "delete", "create", "update", "write", "mutate",
	"command", "send", "trigger", "remove", "reacquire", "retry", "enqueue",
	"publish", "set", "insert", "upsert", "modify", "kill", "restart", "flip",
	"do",
}

// TestClientExposesOnlyReads is the observe-only spine test: it asserts, by
// reflection over the client's method set, that the client type exposes no
// write/command/mutating method — only reads. This realizes #1154's observe-only
// + "operator, no write scope" invariants at the client boundary.
func TestClientExposesOnlyReads(t *testing.T) {
	typ := reflect.TypeOf(&goapi.Client{})
	if typ.NumMethod() == 0 {
		t.Fatal("client exposes no methods; expected at least one read")
	}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		lower := strings.ToLower(name)
		for _, verb := range mutatingVerbs {
			if strings.Contains(lower, verb) {
				t.Errorf("client exposes mutating method %q (matched %q): observe-only violated", name, verb)
			}
		}
		if !readOnlyMethods[name] {
			t.Errorf("client exposes unlisted method %q; if it is a read, add it to readOnlyMethods", name)
		}
	}
}
