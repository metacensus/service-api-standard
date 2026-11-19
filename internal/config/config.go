// Package config reads and validates the service's runtime configuration from
// the process environment at startup.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const defaultPort = 3001

type Config struct {
	Port int
	// DatabaseURL is a secret: it carries the Postgres password.
	DatabaseURL    string
	AllowedOrigins []string
}

// configError carries every problem found in one pass, so a container that refuses
// to start says so once rather than once per missing variable.
type configError struct {
	Problems []string
}

func (e *configError) Error() string {
	return "invalid configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

func Load(getenv func(string) string) (*Config, error) {
	var problems []string

	port := defaultPort
	if raw := strings.TrimSpace(getenv("PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			problems = append(problems, fmt.Sprintf("PORT must be an integer in 1-65535 (got %q)", raw))
		} else {
			port = parsed
		}
	}

	dsn := strings.TrimSpace(getenv("DATABASE_URL"))
	if dsn == "" {
		problems = append(problems, "DATABASE_URL is required")
	}

	var origins []string
	if raw := strings.TrimSpace(getenv("ALLOWED_ORIGINS")); raw == "" {
		problems = append(problems, "ALLOWED_ORIGINS is required")
	} else {
		for _, o := range strings.Split(raw, ",") {
			o = strings.TrimSpace(o)
			if !isOrigin(o) {
				problems = append(problems, fmt.Sprintf("ALLOWED_ORIGINS entry %q must be an origin, scheme://host[:port] with no path", o))
				continue
			}
			origins = append(origins, o)
		}
	}

	if len(problems) > 0 {
		return nil, &configError{Problems: problems}
	}
	return &Config{Port: port, DatabaseURL: dsn, AllowedOrigins: origins}, nil
}

func isOrigin(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme+"://"+u.Host == s && u.Hostname() != ""
}
