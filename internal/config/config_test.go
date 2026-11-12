package config

import (
	"errors"
	"reflect"
	"testing"
)

var valid = map[string]string{"DATABASE_URL": "postgres://db/x", "ALLOWED_ORIGINS": "http://localhost:5173"}

func TestLoad(t *testing.T) {
	tests := []struct {
		name         string
		override     map[string]string
		want         *Config
		wantProblems []string
	}{
		{
			name: "success - port defaults",
			want: &Config{Port: 3001, DatabaseURL: "postgres://db/x", AllowedOrigins: []string{"http://localhost:5173"}},
		},
		{
			name:     "success - port and url are trimmed",
			override: map[string]string{"PORT": " 8080 ", "DATABASE_URL": " postgres://db/x "},
			want:     &Config{Port: 8080, DatabaseURL: "postgres://db/x", AllowedOrigins: []string{"http://localhost:5173"}},
		},
		{
			name:     "success - several trimmed origins",
			override: map[string]string{"ALLOWED_ORIGINS": "https://a.example, http://localhost:5173"},
			want:     &Config{Port: 3001, DatabaseURL: "postgres://db/x", AllowedOrigins: []string{"https://a.example", "http://localhost:5173"}},
		},
		{
			name:         "error - database url blank",
			override:     map[string]string{"DATABASE_URL": "  "},
			wantProblems: []string{"DATABASE_URL is required"},
		},
		{
			name:         "error - allowed origins blank",
			override:     map[string]string{"ALLOWED_ORIGINS": "  "},
			wantProblems: []string{"ALLOWED_ORIGINS is required"},
		},
		{
			name:         "error - one bad origin among good",
			override:     map[string]string{"ALLOWED_ORIGINS": "https://a.example,https://a.example/app"},
			wantProblems: []string{`ALLOWED_ORIGINS entry "https://a.example/app" must be an origin, scheme://host[:port] with no path`},
		},
		{
			name:         "error - trailing comma",
			override:     map[string]string{"ALLOWED_ORIGINS": "https://a.example,"},
			wantProblems: []string{`ALLOWED_ORIGINS entry "" must be an origin, scheme://host[:port] with no path`},
		},
		{
			name:         "error - port not a number",
			override:     map[string]string{"PORT": "no"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "no")`},
		},
		{
			name:         "error - port zero",
			override:     map[string]string{"PORT": "0"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "0")`},
		},
		{
			name:         "error - port too large",
			override:     map[string]string{"PORT": "65536"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "65536")`},
		},
		{
			name:     "error - every problem in one pass",
			override: map[string]string{"PORT": "no", "DATABASE_URL": "", "ALLOWED_ORIGINS": ""},
			wantProblems: []string{
				`PORT must be an integer in 1-65535 (got "no")`,
				"DATABASE_URL is required",
				"ALLOWED_ORIGINS is required",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(func(key string) string {
				if v, ok := tt.override[key]; ok {
					return v
				}
				return valid[key]
			})
			if tt.wantProblems != nil {
				var cfgErr *configError
				if !errors.As(err, &cfgErr) {
					t.Fatalf("err = %v, want *configError", err)
				}
				if !reflect.DeepEqual(cfgErr.Problems, tt.wantProblems) {
					t.Errorf("problems = %q, want %q", cfgErr.Problems, tt.wantProblems)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("config = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsOrigin(t *testing.T) {
	for s, want := range map[string]bool{
		"https://a.example": true, "http://localhost:5173": true, "https://[::1]:8080": true,
		"https://a.example/": false, "https://a.example?": false, "https://a.example#": false,
		"https://u@a.example": false, "https://a.example/app": false, "a.example": false, "": false,
		"https://:443": false, "mailto:x@y": false, "HTTPS://a.example": false,
	} {
		if got := isOrigin(s); got != want {
			t.Errorf("isOrigin(%q) = %v, want %v", s, got, want)
		}
	}
}
