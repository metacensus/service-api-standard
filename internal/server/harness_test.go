package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metacensus/api/go/service"
	"github.com/metacensus/api/go/store"
	"golang.org/x/crypto/bcrypt"
)

// fakeStore stands in for persistence. Embedding the nil interface makes any
// store call a loud panic: these tests reach routing, never the store.
type fakeStore struct{ store.Store }

type harness struct {
	t    *testing.T
	http *httptest.Server
	logs *logBuffer
}

// newHarness serves the shell over api; nil means the real handlers.
func newHarness(t *testing.T, api Registrar) *harness {
	t.Helper()
	if api == nil {
		api = service.New(service.Config{Store: &fakeStore{}, BcryptCost: bcrypt.MinCost})
	}

	logs := &logBuffer{}
	ts := httptest.NewServer(New(0, api, slog.New(slog.NewJSONHandler(logs, nil))).Handler())
	t.Cleanup(ts.Close)
	return &harness{t: t, http: ts, logs: logs}
}

func (h *harness) do(method, path string) (int, string) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.http.URL+path, nil)
	if err != nil {
		h.t.Fatalf("NewRequest: %v", err)
	}
	resp, err := h.http.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}
