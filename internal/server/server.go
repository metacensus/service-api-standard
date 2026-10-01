// Package server is the HTTP shell around the API.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	apiserver "github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
)

const shutdownGrace = 10 * time.Second

// Registrar mounts the API's routes; *service.Handlers is one.
type Registrar interface {
	Register(mux apiserver.Mux, rt *apiserver.Runtime)
}

type Server struct {
	port   int
	api    Registrar
	logger *slog.Logger
}

func New(port int, api Registrar, logger *slog.Logger) *Server {
	return &Server{port: port, api: api, logger: logger}
}

// Handler serves the API under routes.Prefix, behind logging and recovery.
//
// /healthz sits outside the prefix and touches nothing, so a database outage
// cannot fail the process's liveness.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	s.api.Register(apiserver.StdMux{ServeMux: mux}, &apiserver.Runtime{Prefix: routes.Prefix})

	return s.requestLogger(s.recoverPanic(mux))
}

// statusRecorder remembers the status and size a handler wrote, for the log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			s.logger.Info("request completed",
				slog.String("event", "request_completed"),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("elapsed", time.Since(started)),
			)
		}()
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			s.logger.Error("request panicked",
				slog.String("event", "request_panicked"),
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())),
			)
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// Run serves until ctx is cancelled, then drains in-flight requests before
// returning.
func (s *Server) Run(ctx context.Context) error {
	addr := net.JoinHostPort("", strconv.Itoa(s.port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.logger.Info("listening",
		slog.String("event", "listening"),
		slog.Int("port", s.port),
		slog.String("prefix", routes.Prefix),
	)

	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	s.logger.Info("shutting down",
		slog.String("event", "shutting_down"),
		slog.Duration("grace", shutdownGrace))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		// Grace lapsed with requests still running.
		_ = httpServer.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-serveErr
}
