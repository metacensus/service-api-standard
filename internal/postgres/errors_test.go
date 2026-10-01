package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"
	"github.com/metacensus/api/go/store"
)

func TestKindOf_Error(t *testing.T) {
	pg := func(code string) error { return fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code}) }
	errSentinel := errors.New("encode failure")

	tests := []struct {
		name string
		err  error
		want store.Kind
	}{
		{"success - unique violation", pg("23505"), store.AlreadyExists},
		{"success - foreign key violation", pg("23503"), store.InvalidContent},
		{"success - untranslatable character", pg("22P05"), store.InvalidContent},
		{"success - serialization failure", pg("40001"), store.Unavailable},
		{"success - deadlock", pg("40P01"), store.Unavailable},
		{"success - admin shutdown", pg("57P01"), store.Unavailable},
		{"success - connection exception class", pg("08006"), store.Unavailable},
		{"success - no rows", fmt.Errorf("scan: %w", pgx.ErrNoRows), store.NotFound},
		{"success - closed pool", puddle.ErrClosedPool, store.Unavailable},
		{"success - deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), store.Unavailable},
		{"success - cancellation", context.Canceled, store.Unavailable},
		{"success - dropped connection", io.ErrUnexpectedEOF, store.Unavailable},
		{"success - network error", &net.OpError{Op: "read", Err: errors.New("reset")}, store.Unavailable},
		{"success - refused connect", &pgconn.ConnectError{}, store.Unavailable},
		{"success - kind already carried", store.Errf(store.SignatureInvalid, "X", errSentinel), store.SignatureInvalid},
		{"error - unrelated postgres error carries no kind", pg("42P01"), ""},
		{"error - unrelated error carries no kind", errSentinel, ""},
		{"error - nil carries no kind", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kindOf(tt.err); got != tt.want {
				t.Errorf("kindOf(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestFail_Error(t *testing.T) {
	cause := &pgconn.PgError{Code: "23505"}
	other := errors.New("boom")

	tests := []struct {
		name      string
		err       error
		wantKind  store.Kind
		wantCause error
	}{
		{"success - nil stays nil", nil, "", nil},
		{"success - kind keeps its cause for logs", cause, store.AlreadyExists, cause},
		{"success - bare kind passes through", store.Unauthenticated, store.Unauthenticated, nil},
		{"error - unknown error stays kindless but wrapped", other, "", other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fail("Op", tt.err)
			if k := store.KindOf(got); k != tt.wantKind {
				t.Errorf("kind = %q, want %q", k, tt.wantKind)
			}
			if tt.err == nil && got != nil {
				t.Errorf("fail(nil) = %v, want nil", got)
			}
			if tt.wantCause != nil && !errors.Is(got, tt.wantCause) {
				t.Errorf("%v does not wrap %v", got, tt.wantCause)
			}
		})
	}
}
