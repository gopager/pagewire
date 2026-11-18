package observe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gopager/pagewire/internal/buildinfo"
	"github.com/gopager/pagewire/internal/config"
)

// Server serves /metrics, /healthz, /readyz and optionally /debug/pprof.
type Server struct {
	cfg      config.ObservabilityConfig
	registry *prometheus.Registry
	ready    atomic.Bool
	readyMsg atomic.Pointer[string]
}

// NewServer creates the metrics registry and registers the process-wide
// collectors. Nothing listens until Run is called.
func NewServer(cfg config.ObservabilityConfig, instance string) *Server {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pagewire_build_info",
		Help: "Build information; the value is always 1.",
	}, []string{"version", "commit", "instance"})
	info.WithLabelValues(buildinfo.Version, buildinfo.Commit(), instance).Set(1)
	reg.MustRegister(info)
	s := &Server{cfg: cfg, registry: reg}
	msg := "starting"
	s.readyMsg.Store(&msg)
	return s
}

// Registry returns the registry that components register their metrics on.
func (s *Server) Registry() *prometheus.Registry { return s.registry }

// SetReady flips the readiness state. reason is reported by /readyz when
// not ready.
func (s *Server) SetReady(ready bool, reason string) {
	s.ready.Store(ready)
	s.readyMsg.Store(&reason)
}

// Handler returns the HTTP handler so it can be tested without a listener.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.cfg.Metrics {
		mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.ready.Load() {
			_, _ = io.WriteString(w, "ready\n")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready: "+*s.readyMsg.Load()+"\n")
	})
	if s.cfg.Pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	return mux
}

// Run listens on the configured address until ctx is done. An empty listen
// address disables the server.
func (s *Server) Run(ctx context.Context) error {
	if s.cfg.Listen == "" {
		<-ctx.Done()
		return nil
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
