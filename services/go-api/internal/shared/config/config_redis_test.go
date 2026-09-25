package config

import (
	"strings"
	"testing"
)

func TestLoad_RedisURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "unset", url: ""},
		{name: "valid", url: "redis://localhost:6379/0"},
		{name: "padded", url: " redis://localhost:6379", wantErr: true},
		{name: "wrong scheme", url: "http://localhost:6379", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			env["REDIS_URL"] = tc.url
			setEnv(t, env)

			_, err := Load()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "REDIS_URL") {
				t.Fatalf("err = %v, want one naming REDIS_URL", err)
			}
		})
	}
}

func TestLoad_RedisURLErrorRedactsCredentials(t *testing.T) {
	env := feedbackBaseEnv()
	env["REDIS_URL"] = "redis://user:s3cret@host:99999999/x"
	setEnv(t, env)

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error leaks credentials: %v", err)
	}
}
