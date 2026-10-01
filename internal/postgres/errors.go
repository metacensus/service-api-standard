package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"
	"github.com/metacensus/api/go/store"
)

// kindOf is the one place a driver error becomes a store.Kind; "" means the
// error is not one the contract names.
func kindOf(err error) store.Kind {
	if k := store.KindOf(err); k != "" {
		return k
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.NotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch c := pgErr.Code; {
		case c == "23505": // unique_violation
			return store.AlreadyExists
		case c == "23503", c == "22P05": // foreign_key_violation; \u0000 in a jsonb string
			return store.InvalidContent
		case c == "40001", c == "40P01", // serialization_failure, deadlock_detected
			strings.HasPrefix(c, "08"),  // connection exception
			strings.HasPrefix(c, "57P"): // shutdown, crash, cannot connect now
			return store.Unavailable
		}
		return ""
	}
	var connErr *pgconn.ConnectError
	var netErr net.Error
	if errors.As(err, &connErr) || errors.As(err, &netErr) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, puddle.ErrClosedPool) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return store.Unavailable
	}
	return ""
}

// fail attaches op to err for logs, keeping its Kind if it has one; nil stays nil. A bare
// Kind (a refusal raised here) passes through as memstore returns it.
func fail(op string, err error) error {
	if err == nil {
		return nil
	}
	if _, bare := err.(store.Kind); bare {
		return err
	}
	if k := kindOf(err); k != "" {
		return store.Errf(k, op, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
