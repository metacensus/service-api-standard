package server

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apiserver "github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
)

// logBuffer is a goroutine-safe io.Writer.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// panicking mounts one route that panics.
type panicking struct{}

func (panicking) Register(mux apiserver.Mux, rt *apiserver.Runtime) {
	mux.Method(http.MethodGet, rt.Prefix+"/boom", http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("secret detail") }))
}

func TestServer_Handler(t *testing.T) {
	tests := []struct {
		name       string
		api        Registrar
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantLog    string
	}{
		{
			name:       "success - healthz is outside the prefix",
			method:     http.MethodGet,
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   `{"ok":true}`,
			wantLog:    `"event":"request_completed"`,
		},
		{
			name:       "success - api healthcheck is reachable under the prefix",
			method:     http.MethodGet,
			path:       routes.Prefix + "/healthcheck",
			wantStatus: http.StatusOK,
		},
		{
			name:       "error - authenticated route without a token is 401",
			method:     http.MethodGet,
			path:       routes.Prefix + "/self",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "error - api route outside the prefix is 404",
			method:     http.MethodGet,
			path:       "/healthcheck",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "error - healthz does not move under the prefix",
			method:     http.MethodGet,
			path:       routes.Prefix + "/healthz",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "error - healthz is GET only",
			method:     http.MethodPost,
			path:       "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "error - a panicking handler is a 500 that leaks nothing",
			api:        panicking{},
			method:     http.MethodGet,
			path:       routes.Prefix + "/boom",
			wantStatus: http.StatusInternalServerError,
			wantLog:    `"event":"request_panicked"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.api)

			status, body := h.do(tt.method, tt.path)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %q)", status, tt.wantStatus, body)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
			if strings.Contains(body, "secret detail") {
				t.Errorf("body leaked the panic value: %q", body)
			}
			if !strings.Contains(h.logs.String(), tt.wantLog) {
				t.Errorf("log = %q, want it to contain %q", h.logs.String(), tt.wantLog)
			}
		})
	}
}

// blocking mounts a route that signals it has started, then holds until
// released.
type blocking struct{ started, release chan struct{} }

func (b blocking) Register(mux apiserver.Mux, rt *apiserver.Runtime) {
	mux.Method(http.MethodGet, rt.Prefix+"/slow", http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			close(b.started)
			<-b.release
		}))
}

func TestServer_Run(t *testing.T) {
	// Reserve a free port, then release it for Run to bind.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	api := blocking{started: make(chan struct{}), release: make(chan struct{})}
	srv := New(port, api, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// No keep-alive: Shutdown waits up to 5s on a connection that never sent a request.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("healthz = %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	slowStatus := make(chan int, 1)
	go func() {
		resp, err := client.Get(base + routes.Prefix + "/slow")
		if err != nil {
			slowStatus <- 0
			return
		}
		resp.Body.Close()
		slowStatus <- resp.StatusCode
	}()
	<-api.started

	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run returned %v while a request was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(api.release)

	if got := <-slowStatus; got != http.StatusOK {
		t.Errorf("in-flight request = %d, want 200", got)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the request finished")
	}
}
