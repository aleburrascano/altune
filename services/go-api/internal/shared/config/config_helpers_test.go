package config

import "testing"

func TestConfig_HasRedis(t *testing.T) {
	tests := []struct {
		name     string
		redisURL string
		want     bool
	}{
		{name: "set", redisURL: "redis://localhost:6379", want: true},
		{name: "empty", redisURL: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{RedisURL: tt.redisURL}
			if got := cfg.HasRedis(); got != tt.want {
				t.Errorf("HasRedis() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasLastFM(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "set", key: "abc123", want: true},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LastFMAPIKey: tt.key}
			if got := cfg.HasLastFM(); got != tt.want {
				t.Errorf("HasLastFM() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasFanartTV(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "set", key: "fanart-key", want: true},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{FanartTVAPIKey: tt.key}
			if got := cfg.HasFanartTV(); got != tt.want {
				t.Errorf("HasFanartTV() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasGenius(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "set", token: "genius-token", want: true},
		{name: "empty", token: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{GeniusAccessToken: tt.token}
			if got := cfg.HasGenius(); got != tt.want {
				t.Errorf("HasGenius() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasMusicBrainz(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{name: "set", ua: "altune/0.1 ( mailto:dev@test.com )", want: true},
		{name: "empty", ua: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{MusicBrainzUserAgent: tt.ua}
			if got := cfg.HasMusicBrainz(); got != tt.want {
				t.Errorf("HasMusicBrainz() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "development", env: "development", want: true},
		{name: "production", env: "production", want: false},
		{name: "staging", env: "staging", want: false},
		{name: "empty", env: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env}
			if got := cfg.IsDevelopment(); got != tt.want {
				t.Errorf("IsDevelopment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasOCIS3_AllCombinations(t *testing.T) {
	// The existing test covers all-set and missing-bucket.
	// Cover the other missing-field cases.
	tests := []struct {
		name     string
		endpoint string
		access   string
		secret   string
		bucket   string
		want     bool
	}{
		{name: "all set", endpoint: "e", access: "a", secret: "s", bucket: "b", want: true},
		{name: "missing endpoint", endpoint: "", access: "a", secret: "s", bucket: "b", want: false},
		{name: "missing access key", endpoint: "e", access: "", secret: "s", bucket: "b", want: false},
		{name: "missing secret key", endpoint: "e", access: "a", secret: "", bucket: "b", want: false},
		{name: "missing bucket", endpoint: "e", access: "a", secret: "s", bucket: "", want: false},
		{name: "all empty", endpoint: "", access: "", secret: "", bucket: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				OCIS3Endpoint:  tt.endpoint,
				OCIS3AccessKey: tt.access,
				OCIS3SecretKey: tt.secret,
				OCIS3Bucket:    tt.bucket,
			}
			if got := cfg.HasOCIS3(); got != tt.want {
				t.Errorf("HasOCIS3() = %v, want %v", got, tt.want)
			}
		})
	}
}
