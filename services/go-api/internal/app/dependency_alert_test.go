package app

import (
	"context"
	"testing"
)

func TestBuildDependencyCondition(t *testing.T) {
	ctx := context.Background()
	up := DependencyHealth{DB: DepUp, Redis: DepUp, Auth: DepUp}

	cases := []struct {
		name    string
		health  DependencyHealth
		wantMsg string
	}{
		{"auth only down names auth", DependencyHealth{DB: DepUp, Redis: DepUp, Auth: DepDown}, "dependencies down: auth"},
		{"db only down names db", DependencyHealth{DB: DepDown, Redis: DepUp, Auth: DepUp}, "dependencies down: db"},
		{"all down names all", DependencyHealth{DB: DepDown, Redis: DepDown, Auth: DepDown}, "dependencies down: db redis auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := buildDependencyCondition(func(context.Context) DependencyHealth { return tc.health })
			if cond.Key != "dependency_down" {
				t.Fatalf("key = %q, want dependency_down", cond.Key)
			}
			alert := cond.Eval(ctx)
			if alert == nil {
				t.Fatal("alert = nil, want it to fire")
			}
			if alert.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", alert.Message, tc.wantMsg)
			}
		})
	}

	t.Run("healthy does not fire", func(t *testing.T) {
		cond := buildDependencyCondition(func(context.Context) DependencyHealth { return up })
		if alert := cond.Eval(ctx); alert != nil {
			t.Fatalf("alert = %+v, want nil", alert)
		}
	})
}
