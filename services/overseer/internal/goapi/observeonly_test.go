package goapi_test

import (
	"altune/overseer/internal/goapi"
	"reflect"
	"strings"
	"testing"
)

var readOnlyMethods = map[string]bool{
	"Health":                  true,
	"AdminHealth":             true,
	"AdminEval":               true,
	"AdminAcquisition":        true,
	"AdminMetricsLive":        true,
	"AdminProviderUsage":      true,
	"AdminDiscographyQuality": true,
}

var mutatingVerbs = []string{
	"post", "put", "patch", "delete", "create", "update", "write", "mutate",
	"command", "send", "trigger", "remove", "reacquire", "retry", "enqueue",
	"publish", "set", "insert", "upsert", "modify", "kill", "restart", "flip",
	"do",
}

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
