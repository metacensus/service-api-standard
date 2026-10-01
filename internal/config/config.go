// Package config reads and validates the service's runtime configuration from
// the process environment at startup.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

const DefaultPort = 3001

type Config struct {
	Port int
	// DatabaseURL is a secret: it carries the Postgres password.
	DatabaseURL string
}

// Error carries every problem found in one pass, so a container that refuses
// to start says so once rather than once per missing variable.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "invalid configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

func Load(getenv func(string) string) (*Config, error) {
	var problems []string

	port := DefaultPort
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

	if len(problems) > 0 {
		return nil, &Error{Problems: problems}
	}
	return &Config{Port: port, DatabaseURL: dsn}, nil
}
