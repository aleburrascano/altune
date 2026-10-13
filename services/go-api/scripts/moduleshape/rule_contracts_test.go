package main

import "testing"

func TestCollectG9(t *testing.T) {
	fake := "internal/m/mtest/track_fake.go"
	runCases(t, []checkCase{
		{"fake with contract", fullModule("m", map[string]string{fake: goSource(), "internal/m/mtest/track_contract.go": goSource()}), nil},
		{"fake without contract", fullModule("m", map[string]string{fake: goSource()}), []string{"G9 " + fake + " has no track_contract.go"}},
		{"contract of another stem", fullModule("m", map[string]string{fake: goSource(), "internal/m/mtest/other_contract.go": goSource()}), []string{"G9 " + fake + " has no track_contract.go"}},
		{"fake outside test package", fullModule("m", map[string]string{"internal/m/adapters/track_fake.go": goSource()}), nil},
		{"test file is not a fake", fullModule("m", map[string]string{"internal/m/mtest/track_fake_test.go": goSource()}), nil},
	})
}
