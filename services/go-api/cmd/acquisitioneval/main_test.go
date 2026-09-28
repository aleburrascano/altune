package main

import (
	"reflect"
	"testing"
)

func TestSubcommand_DispatchesCaptureOnlyOnCaptureFirstArg(t *testing.T) {
	tests := map[string]struct {
		args     []string
		wantName string
		wantRest []string
	}{
		"no args beyond the binary name": {
			args:     []string{"acquisitioneval"},
			wantName: "",
			wantRest: nil,
		},
		"default flags, no subcommand": {
			args:     []string{"acquisitioneval", "-goldens", "testdata"},
			wantName: "",
			wantRest: []string{"-goldens", "testdata"},
		},
		"capture first arg": {
			args:     []string{"acquisitioneval", "capture", "-tier", "staging"},
			wantName: "capture",
			wantRest: []string{"-tier", "staging"},
		},
		"capture appearing later is not a subcommand": {
			args:     []string{"acquisitioneval", "-v", "capture"},
			wantName: "",
			wantRest: []string{"-v", "capture"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotName, gotRest := subcommand(tc.args)
			if gotName != tc.wantName {
				t.Fatalf("subcommand(%v) name = %q, want %q", tc.args, gotName, tc.wantName)
			}
			if len(gotRest) == 0 && len(tc.wantRest) == 0 {
				return
			}
			if !reflect.DeepEqual(gotRest, tc.wantRest) {
				t.Fatalf("subcommand(%v) rest = %v, want %v", tc.args, gotRest, tc.wantRest)
			}
		})
	}
}
