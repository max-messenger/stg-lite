package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type SystemServer struct {
	http.Server

	logger *zap.Logger

	systemHandlers []map[string]http.HandlerFunc
}

func NewSystemServer(logger *zap.Logger, cfg Config, opts ...SystemServerOption) *SystemServer {
	router := chi.NewRouter()
	router.Route("/health", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok")) // nolint:errcheck
		})
	})
	router.Route("/metrics", func(r chi.Router) {
		r.Get("/", promhttp.Handler().ServeHTTP)
	})

	router.Route("/debug", func(r chi.Router) {
		r.Mount("/", chiMiddleware.Profiler())
		r.HandleFunc("/block", blockHandleFunc)
		r.HandleFunc("/mutex", mutexHandleFunc)
	})

	s := &SystemServer{
		Server: http.Server{
			Addr:              cfg.Addr,
			Handler:           router,
			ReadHeaderTimeout: time.Second * 3,
		},
		logger: logger,
	}

	for _, opt := range opts {
		opt(s)
	}

	for _, handlers := range s.systemHandlers {
		for path, handler := range handlers {
			router.Mount(path, handler)
		}
	}

	return s
}

func (s *SystemServer) Start(_ context.Context) error {
	ln, err := net.Listen("tcp", s.Addr) // nolint:noctx
	if err != nil {
		return err
	}

	s.logger.Info(
		"Starting System HTTP server",
		zap.String("addr", s.Addr),
	)

	go func() {
		if err := s.Serve(ln); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				s.logger.Info("System Server shutdown")

				return
			}
			s.logger.Error("Failed to start System HTTP server", zap.Error(err))
		}
	}()

	return nil
}

func (s *SystemServer) Stop(ctx context.Context) error {
	return s.Shutdown(ctx)
}

func blockHandleFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET method supported", http.StatusMethodNotAllowed)

		return
	}

	if errParse := r.ParseForm(); errParse != nil { // nolint: gosec
		http.Error(w, "can't parse form", http.StatusBadRequest)

		return
	}

	value := r.Form.Get("value")

	rate, errAtoi := strconv.Atoi(value)
	if errAtoi != nil {
		http.Error(w, "can't parse value", http.StatusBadRequest)

		return
	}

	runtime.SetBlockProfileRate(rate)
	if _, errWrite := w.Write([]byte("block rate updated")); errWrite != nil {
		return
	}
}

func mutexHandleFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET method supported", http.StatusMethodNotAllowed)

		return
	}

	if errParse := r.ParseForm(); errParse != nil { // nolint: gosec
		http.Error(w, "can't parse form", http.StatusBadRequest)

		return
	}

	value := r.Form.Get("value")

	rate, errAtoi := strconv.Atoi(value)
	if errAtoi != nil {
		http.Error(w, "can't parse value", http.StatusBadRequest)

		return
	}

	runtime.SetMutexProfileFraction(rate)
	if _, errWrite := w.Write([]byte("mutex fraction updated")); errWrite != nil {
		return
	}
}
