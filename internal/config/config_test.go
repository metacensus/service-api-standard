package config

import (
	"errors"
	"reflect"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		want         *Config
		wantProblems []string
	}{
		{
			name: "success - port defaults",
			env:  map[string]string{"DATABASE_URL": "postgres://db/x"},
			want: &Config{Port: 3001, DatabaseURL: "postgres://db/x"},
		},
		{
			name: "success - port and url are trimmed",
			env:  map[string]string{"PORT": " 8080 ", "DATABASE_URL": " postgres://db/x "},
			want: &Config{Port: 8080, DatabaseURL: "postgres://db/x"},
		},
		{
			name:         "error - database url blank",
			env:          map[string]string{"DATABASE_URL": "  "},
			wantProblems: []string{"DATABASE_URL is required"},
		},
		{
			name:         "error - port not a number",
			env:          map[string]string{"PORT": "no", "DATABASE_URL": "postgres://db/x"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "no")`},
		},
		{
			name:         "error - port zero",
			env:          map[string]string{"PORT": "0", "DATABASE_URL": "postgres://db/x"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "0")`},
		},
		{
			name:         "error - port too large",
			env:          map[string]string{"PORT": "65536", "DATABASE_URL": "postgres://db/x"},
			wantProblems: []string{`PORT must be an integer in 1-65535 (got "65536")`},
		},
		{
			name: "error - every problem in one pass",
			env:  map[string]string{"PORT": "no"},
			wantProblems: []string{
				`PORT must be an integer in 1-65535 (got "no")`,
				"DATABASE_URL is required",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(func(key string) string { return tt.env[key] })
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
