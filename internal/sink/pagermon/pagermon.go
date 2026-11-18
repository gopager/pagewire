// Package pagermon delivers messages to a PagerMon server through its
// POST /api/messages endpoint, in the shape the stock PagerMon client
// produces (see compat.go).
package pagermon

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gopager/pagewire/internal/buildinfo"
	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sink"
)

func init() {
	sink.Register("pagermon", New)
}

// Sink posts to one PagerMon server.
type Sink struct {
	name   string
	cfg    config.PagermonConfig
	apiKey string
	env    sink.Env
	client *http.Client
	url    string
}

// New builds the sink from a target configuration.
func New(cfg config.TargetConfig, env sink.Env) (sink.Sink, error) {
	if cfg.Pagermon == nil {
		return nil, errors.New("pagermon: missing pagermon block")
	}
	key, err := config.ResolveSecret(cfg.Pagermon.APIKey, cfg.Pagermon.APIKeyFile)
	if err != nil {
		return nil, fmt.Errorf("pagermon: target %s: %w", cfg.Name, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Pagermon.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit operator choice
	}
	timeout := cfg.Pagermon.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Sink{
		name:   cfg.Name,
		cfg:    *cfg.Pagermon,
		apiKey: key,
		env:    env,
		client: &http.Client{Transport: transport, Timeout: timeout},
		url:    strings.TrimRight(cfg.Pagermon.URL, "/"),
	}, nil
}

// Name implements sink.Sink.
func (s *Sink) Name() string { return s.name }

// Type implements sink.Sink.
func (s *Sink) Type() string { return "pagermon" }

// source expands the source template.
func (s *Sink) source(msg *message.Message) string {
	r := strings.NewReplacer(
		"{channel}", msg.Channel.Name,
		"{frequency}", config.Frequency(msg.Channel.FreqHz).String(),
		"{protocol}", msg.Protocol,
		"{hostname}", s.env.Hostname,
		"{instance}", s.env.Instance,
	)
	return r.Replace(s.cfg.Source)
}

// Deliver implements sink.Sink.
func (s *Sink) Deliver(ctx context.Context, msg *message.Message) (sink.Outcome, error) {
	body, ok := s.Build(msg)
	if !ok {
		return sink.OutcomeSkipped, nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return sink.OutcomeRejected, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/api/messages", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := s.client.Do(req)
	if err != nil {
		return "", &sink.Error{Class: sink.ClassNetwork, Err: err}
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	reply := strings.TrimSpace(string(text))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		switch {
		case strings.HasPrefix(reply, "Ignoring duplicate"):
			return sink.OutcomeDuplicate, nil
		case strings.HasPrefix(reply, "Ignoring filtered"):
			return sink.OutcomeFiltered, nil
		default:
			return sink.OutcomeDelivered, nil
		}
	case resp.StatusCode == http.StatusBadRequest, resp.StatusCode == http.StatusUnprocessableEntity,
		resp.StatusCode == http.StatusInternalServerError && strings.Contains(reply, "address or message missing"):
		// The server answers 500 (not 4xx) when a field is missing; that is
		// a final rejection, not an outage.
		return sink.OutcomeRejected, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", &sink.Error{Class: sink.ClassAuth, Err: fmt.Errorf("pagermon: HTTP %d: %s", resp.StatusCode, truncate(reply, 200))}
	default:
		return "", &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon: HTTP %d: %s", resp.StatusCode, truncate(reply, 200))}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// Check implements sink.Sink by fetching the message list, which the server
// serves without authentication unless apiSecurity is on; the API key is
// sent so that the check works either way.
func (s *Sink) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/api/messages?limit=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := s.client.Do(req)
	if err != nil {
		return &sink.Error{Class: sink.ClassNetwork, Err: err}
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon: HTTP %d", resp.StatusCode)}
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return &sink.Error{Class: sink.ClassAuth, Err: fmt.Errorf("pagermon: HTTP %d", resp.StatusCode)}
	}
	return nil
}
