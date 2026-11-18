// Package observe provides logging, Prometheus metrics and the HTTP health
// endpoints of the gateway.
package observe

import (
	"io"
	"log/slog"

	"github.com/gopager/pagewire/internal/config"
)

// NewLogger builds a slog.Logger from the configuration, writing to w. The
// instance name is attached to every record so logs from several instances
// on one host can be told apart.
func NewLogger(cfg config.LogConfig, instance string, w io.Writer) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h).With("instance", instance)
}
